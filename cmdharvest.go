package main


import (
    "fmt"
    "os"
    "os/exec"
    "path"
    "flag"
    "time"
    "path/filepath"
    "log"
)


// ---------------------------------------------------------------------------------------------------
// Harvest commands
//      Extract the records from a provider and store them in a directory.

type HarvestCommand struct {
    Ctx                 *Context
    dryRun              *bool
    listAndGet          *bool
    compressDirs        *bool
    setName             *string
    beforeDate          *string
    afterDate           *string
    fromFile            *string
    filenameFilter      *string
    filenameFilterAst   RSExprAst
    firstResult         *int
    maxResults          *int
    maxDirSize          *int
    downloadWorkers     *int
    dirPrefix           string
    recordCount         int
    lastDirId           int
}

// Get list identifier arguments
func (lc *HarvestCommand) genListIdentifierArgsFromCommandLine() ListIdentifierArgs {
    var set string

    set = *(lc.setName)
    if set == "" {
        set = lc.Ctx.Provider.Set
    } else if set == "*" {
        set = ""
    }

    args := ListIdentifierArgs{
        Set: set,
        From: parseDateString(*(lc.afterDate)),
        Until: parseDateString(*(lc.beforeDate)),
    }

    return args
}

// Returns the name of directory given the directory ID
func (lc *HarvestCommand) dirName(dirId int) string {
    return fmt.Sprintf("%s/%02d", lc.dirPrefix, dirId)
}

// Saves the record
func (lc *HarvestCommand) saveRecordToDir(dirId int, res *RecordResult) {
    dir := lc.dirName(dirId)

    // The filename to use.  If there's a filter, execute it and use the returned string
    // as the filename.  Otherwise, simply use the records URN
    var resId = res.Identifier()
    var filename string = resId

    if lc.filenameFilterAst != nil {
        res, err := lc.filenameFilterAst.Evaluate(res)
        if (err == nil) && (res != nil) && (res.Bool()) {
            filename = res.String()
        } else if (err != nil) {
            log.Printf("%s: error in filename filter, using the URN: %s", resId, err.Error())
        } else {
            log.Printf("%s: warn: filename filter returned false, using the URN", resId)
        }
    }

    // Escape filenames to avoid invalid characters such as '/' causing
    // potential file naming problems.
    fileBaseName := EscapeIdForFilename(filename)
    if fileBaseName == "" {
        log.Println("warn: using file basename '__empty__' for record with an empty identifier")
        fileBaseName = "__empty__"
    }

    outFile := filepath.Join(dir, fileBaseName + ".xml")

    os.MkdirAll(dir, 0755)

    file, err := os.Create(outFile)
    if err != nil {
        panic(err)
    }
    defer file.Close()

    file.WriteString(res.Content)
}

// Close the current directory before creating and writing to a new one
func (lc *HarvestCommand) closeDir(dirId int) {
    // Do nothing if this is a dry run
    if *(lc.dryRun) {
        return
    }

    dir := lc.dirName(dirId)
    if *(lc.compressDirs) {
        base := path.Base(dir)
        parent := path.Dir(dir)

        if (lc.Ctx.LogLevel >= TraceLogLevel) {
            log.Printf("Compressing %s -> %s", base, dir + ".zip")
        }

        cmd := exec.Command("zip", "-m", "-r", base + ".zip", base)
        cmd.Dir = parent
        err := cmd.Start()
        if (err != nil) {
            fmt.Fprintf(os.Stderr, "Cannot compress '%s'\n", dir)
        }
    }
}

func (lc *HarvestCommand) saveRecord(res *RecordResult) {
    lc.recordCount++
    dirId := (lc.recordCount / *(lc.maxDirSize)) + 1
    if (dirId != lc.lastDirId) {
        lc.closeDir(lc.lastDirId)
        lc.lastDirId = dirId
    }

    if (lc.Ctx.LogLevel >= DebugLogLevel) {
        log.Printf("%8d  %s\n", lc.recordCount, res.Identifier())
    }
    if ((lc.recordCount % 1000) == 0) {
        log.Printf("Harvested %d records\n", lc.recordCount)
    }

    if (! *(lc.dryRun)) {
        lc.saveRecordToDir(dirId, res)
    }
}


// Contract with the HarvesterObserver

func (lc *HarvestCommand) OnRecord(rr *RecordResult) {
    lc.saveRecord(rr)
}

func (lc *HarvestCommand) OnError(err error) {
    log.Printf("ERROR: %s\n", err)
}

func (lc *HarvestCommand) OnCompleted(harvested int, skipped int, errors int) {
    log.Printf("Finished: %d records harvested, %d records skipped, %d errors", harvested, skipped, errors)
}

// Harvest the records using a specific harvester
func (lc *HarvestCommand) harvestWithHarvester(harvester Harvester) {
    harvester.Harvest(lc)
}

// List the identifiers from a provider
func (lc *HarvestCommand) harvest() {
    var harvester Harvester
    args := lc.genListIdentifierArgsFromCommandLine()

    if *(lc.fromFile) != "" {
        // Setup a map-reduce queue for fetching responses in parallel
        harvester = &FileHarvester{
            Session:        lc.Ctx.Session,
            Filename:       *(lc.fromFile),
            FirstResult:    *(lc.firstResult),
            MaxResults:     *(lc.maxResults),
            Workers:        *(lc.downloadWorkers),
            Guard:          LiveRecordsPredicate,
        }
    } else if *(lc.listAndGet) {
        // Get the list and pass it to the getters in parallel
        harvester = &ListAndGetRecordHarvester{
            Session:        lc.Ctx.Session,
            ListArgs:       args,
            FirstResult:    *(lc.firstResult),
            MaxResults:     *(lc.maxResults),
            Workers:        *(lc.downloadWorkers),
            HarvestGuard:   LiveRecordsHeaderPredicate,
            Guard:          LiveRecordsPredicate,
        }
    } else {
        harvester = &ListRecordHarvester{
            Session:        lc.Ctx.Session,
            ListArgs:       args,
            FirstResult:    *(lc.firstResult),
            MaxResults:     *(lc.maxResults),
            Guard:          LiveRecordsPredicate,
        }
    }

    lc.harvestWithHarvester(harvester)
}

func (lc *HarvestCommand) Flags(fs *flag.FlagSet) *flag.FlagSet {
    lc.setName = fs.String("s", "", "Select records from this set")
    lc.dryRun = fs.Bool("n", false, "Dry run.  Do not save results.")
    lc.listAndGet = fs.Bool("L", false, "Use list and get instead of ListRecord")
    lc.beforeDate = fs.String("B", "", "Select records that were updated before date (YYYY-MM-DD)")
    lc.afterDate = fs.String("A", "", "Select records that were updated after date (YYYY-MM-DD)")
    lc.firstResult = fs.Int("f", 0, "Index of first record to retrieve")
    lc.fromFile = fs.String("F", "", "Read identifiers from a file")
    lc.maxResults = fs.Int("c", 100000, "Maximum number of records to retrieve")
    lc.maxDirSize = fs.Int("D", 10000, "Maximum number of files to store in each directory")
    lc.compressDirs = fs.Bool("C", false, "Compress directories once they are full")
    lc.downloadWorkers = fs.Int("W", 4, "Number of download workers running in parallel")

    // Advanded options
    lc.filenameFilter = fs.String("N", "", "Use rs-expression for filename")

    return fs
}

func (lc *HarvestCommand) Run(args []string) {
    // Compile the filename filter if there is one
    if *lc.filenameFilter != "" {
        var err error
        lc.filenameFilterAst, err = ParseRSExpr(*lc.filenameFilter)
        if err != nil {
            log.Fatal("Error in filename filter: ", err)
        }
    }

    lc.lastDirId = 1
    lc.dirPrefix = time.Now().Format("20060102T150405")
    lc.harvest()
    lc.closeDir(lc.lastDirId)

}
