// Record search.  Parses a search expression and runs it over a RecordResult returning true if the
// record matches the expression, or false otherwise.
//
//

package main

import (
    "text/scanner"
    "strconv"
    "strings"
    "bytes"
    "fmt"

    "launchpad.net/xmlpath"
)


type RecordSearcher interface {

    // Searches the record.
    SearchRecord(rr *RecordResult) (bool, string, error)
}

// A record searcher which uses a parsed record search expression
type ExprRecordSearcher struct {
    ast     RSExprAst
}

func (ers *ExprRecordSearcher) SearchRecord(rr *RecordResult) (bool, string, error) {
    res, err := ers.ast.Evaluate(rr)
    if err != nil {
        return false, "", err
    } else {
        return res.Bool(), res.String(), nil
    }
}

// ------------------------------------------------------------------------------
// Values

// Execution value types
type RSExprValue    interface {

    // Various conversion methods
    Bool() bool
    String() string
}

// A string value
type RSString       string

func (s RSString) Bool() bool {
    return (string(s) != "")
}

func (s RSString) String() string {
    return string(s)
}


// A boolean value
type RSBool         bool

func (b RSBool) Bool() bool {
    return bool(b)
}

func (b RSBool) String() string {
    if (bool(b)) {
        return "true"
    } else {
        return "false"
    }
}

// Native function types
type RSNativeFunction   func(rr *RecordResult, args []RSExprValue) (RSExprValue, error)

// ------------------------------------------------------------------------------
//

// AST nodes.
//
type RSExprAst interface {

    // Evaulates the result.
    Evaluate(rr *RecordResult) (RSExprValue, error)
}


// A function call.
//
type RSExprFnCall struct {
    Fn          RSNativeFunction
    FnArgs      []RSExprAst
}

func (fnCall *RSExprFnCall) Evaluate(rr *RecordResult) (RSExprValue, error) {
    // Get all the sub-expressions results
    argValues := make([]RSExprValue, len(fnCall.FnArgs))
    for i := range fnCall.FnArgs {
        val, err := fnCall.FnArgs[i].Evaluate(rr)
        if err != nil {
            return nil, err
        }
        argValues[i] = val
    }

    // Invoke the function
    return fnCall.Fn(rr, argValues)
}


// A string literal
//
type RSExprLiteral struct {
    val     RSExprValue
}

func (lt RSExprLiteral) Evaluate(rr *RecordResult) (RSExprValue, error) {
    return lt.val, nil
}

// ------------------------------------------------------------------------------
//

type etoken struct {
    Expected        rune
    Actual          rune
}

func (e *etoken) Error() string {
    return fmt.Sprintf("Expected %s but got %s", scanner.TokenString(e.Expected), scanner.TokenString(e.Actual))
}

// Record search parser
type recordSearchParser struct {
    scan        *scanner.Scanner
    tok         rune
    tokText     string
}

// Gets the next token
func (rsp *recordSearchParser) nextToken() {
    if (rsp.tok != scanner.EOF) {
        rsp.tok = rsp.scan.Scan()
        rsp.tokText = rsp.scan.TokenText()
    }
}

// Returns true if the next token is a specific token
func (rsp *recordSearchParser) nextTokenIs(tok rune) bool {
    return (rsp.tok == tok)
}

// Consumes a token.  Returns the token value if it was successfully consumed, or an EToken
// if it wasn't consumed.
func (rsp *recordSearchParser) consume(tok rune) (txt string, err error) {
    if (rsp.tok == tok) {
        txt = rsp.tokText
        rsp.nextToken()
    } else {
        err = &etoken{tok, rsp.tok}
    }
    return
}

// Parses an expression
//      <expr>  =   <fncall> | <atom>
func (rsp *recordSearchParser) parseExpr() (RSExprAst, error) {
    if (rsp.tok == scanner.Ident) {
        return rsp.parseFn()
    } else {
        return rsp.parseAtom()
    }
}

// Parses an atom
//      <atom>  =   STRING
func (rsp *recordSearchParser) parseAtom() (RSExprAst, error) {
    str, err := rsp.readString()
    return RSExprLiteral{RSString(str)}, err
}

// Parses a function call
//      <fncall>    =   <IDENT> [ "(" (<expr> ("," <expr>)*)? ")" ]
func (rsp *recordSearchParser) parseFn() (RSExprAst, error) {
    fnName, err := rsp.consume(scanner.Ident)
    if (err != nil) {
        return nil, err
    }

    // Look up the function
    fn, hasFn := NATIVE_FUNCTIONS[fnName]
    if !hasFn {
        return nil, fmt.Errorf("No such function: %s", fnName)
    }

    if rsp.nextTokenIs('(') {
        // Function with arguments
        rsp.consume('(')

        args := make([]RSExprAst, 0)
        for rsp.tok != ')' {
            if len(args) > 0 {
                if _, err = rsp.consume(',') ; err != nil {
                    return nil, err
                }
            }

            if arg, err := rsp.parseExpr() ; err != nil {
                return nil, err
            } else {
                args = append(args, arg)
            }
        }

        if _, err = rsp.consume(')') ; err != nil {
            return nil, err
        }

        return &RSExprFnCall{fn, args}, nil
    } else {
        // Function without arguments
        return &RSExprFnCall{fn, make([]RSExprAst, 0)}, nil
    }
}

// Reads a string value
func (rsp *recordSearchParser) readString() (string, error) {
    if (rsp.tok == scanner.String) || (rsp.tok == scanner.RawString) {
        s, err := strconv.Unquote(rsp.tokText)
        if err != nil {
            return "", err
        } else {
            rsp.consume(rsp.tok)
            return s, nil
        }
    } else {
        return "", fmt.Errorf("Expected string but got %s\n", scanner.TokenString(rsp.tok))
    }
}

// Parses a record match expression
func ParseRecordMatchExpr(expr string) (*ExprRecordSearcher, error) {
    ast, err := ParseRSExpr(expr)
    if err == nil {
        return &ExprRecordSearcher{ast}, nil
    } else {
        return nil, err
    }
}

// Parses an RS expresison
func ParseRSExpr(expr string) (RSExprAst, error) {
    rsp := &recordSearchParser{}
    rsp.scan = new(scanner.Scanner)
    rsp.scan.Init(strings.NewReader(expr))
    rsp.scan.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanRawStrings | scanner.SkipComments
    rsp.nextToken()

    ast, err := rsp.parseExpr()
    if err == nil {
        return ast, nil
    } else {
        return nil, err
    }
}

// -----------------------------------------------------------------------------
// Native functions

var NATIVE_FUNCTIONS = map[string]RSNativeFunction {

    // xp(<xpath>)
    //      Returns the result of running the XPath expression over the record.  The resulting
    //      string is trimmed.
    "xp": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        if (len(args) != 1) {
            return nil, fmt.Errorf("xp() expects exactly 1 argument")
        }

        path, err := xmlpath.Compile(args[0].String())
        if (err != nil) {
            return nil, err
        }

        n, err := xmlpath.Parse(strings.NewReader(rr.Content))
        if (err != nil) {
            return nil, err
        }

        val, _ := path.String(n)
        return RSString(strings.TrimSpace(val)), nil
    },

    // concat(<strs>...)
    //      Returns a string with all the other strings concatinated
    "concat": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        buf := new(bytes.Buffer)
        for _, arg := range args {
            buf.WriteString(arg.String())
        }
        return RSString(buf.String()), nil
    },

    // startsWith(<str>, <prefix>)
    //      Returns the string if it starts with the specific prefix.  Otherwise, returns
    //      the empty string.
    "startsWith": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        if (len(args) != 2) {
            return nil, fmt.Errorf("startsWith() expects exactly 2 argument")
        }

        if (strings.HasPrefix(args[0].String(), args[1].String())) {
            return args[0], nil
        } else {
            return RSString(""), nil
        }
    },

    // contains(<str>, <substring>)
    //      Returns the string if it contains the substring.  Otherwise, returns the empty
    //      string
    "contains": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        if (len(args) != 2) {
            return nil, fmt.Errorf("contains() expects exactly 2 argument")
        }        

        if (strings.Contains(args[0].String(), args[1].String())) {
            return args[0], nil
        } else {
            return RSString(""), nil
        }
    },

    // urn()
    //      Returns the URN of the record
    "urn": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        return RSString(rr.Identifier()), nil
    },

    // replace(<str>, <substr>, <new>)
    //      Replaces all occurances of <substr> found in <str> with <new>
    "replace": func(rr *RecordResult, args []RSExprValue) (RSExprValue, error) {
        if (len(args) != 3) {
            return nil, fmt.Errorf("contains() expects exactly 3 argument")
        }

        return RSString(strings.Replace(args[0].String(), args[1].String(), args[2].String(), -1)), nil
    },
}
