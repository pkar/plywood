package plywood

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents log levels which are sorted based on the underlying number associated to it.
type Level uint8

// Definitions of available levels: Debug < Info < Warning < Error.
const (
	DEBUG Level = iota
	INFO
	WARNING
	ERROR
	FATAL
)

const (
	logglyUrl = "https://urltologgly/key"
)

var (
	program            = filepath.Base(os.Args[0])
	host               = "unknownhost"
	userName           = "unknownuser"
	pid                = os.Getpid()
	severityChars      = [5]rune{'D', 'I', 'W', 'E', 'F'}
	timeNow            = time.Now // Stubbed out for testing.
	logglyEnvironments = map[string]bool{
		"production": true,
		"staging":    true,
	}
)

// LogglyPost is the json representation of what to send
// to loggly.
type LogglyPost struct {
	Timestamp string      `json:"timestamp"` // loggly iso8601 timestamp
	Env       string      `json:"env"`       // environment
	App       string      `json:"app"`       // application name
	Caller    string      `json:"caller"`    // the package.function.linenum
	Host      string      `json:"host"`      // hostname
	Pid       int         `json:"pid"`       // processid
	Level     string      `json:"level"`     // severity level character
	Msg       interface{} `json:"msg"`       // logging event message
}

// Abstraction of log event sender.
type Sender interface {
	Send(string, interface{}) error
}

// Console implements sender and logs events to the console.
type Console struct {
	Env string
	App string
	w   io.Writer
	m   *sync.Mutex
}

// Loggly contains the meta for sending log events to loggly.
// Loggly implements sender.
type Loggly struct {
	Client *http.Client
	Env    string
	App    string
	Host   string
	url    string
}

// Log contains the set loggers. Log output will be sent to
// and Log.Loggers defined (loggly, stderr, stdout)
type Log struct {
	Host      string
	App       string
	Env       string
	Loggers   map[string]Sender
	level     Level
	toStderr  bool
	toLoggly  bool
	toLogglya bool // async loggly posts
}

// global logger created on package initialization.
var logger *Log

// shortHostname returns its argument, truncating at the first period.
// For instance, given "www.google.com" it returns "www".
func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

func init() {
	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}
	current, err := user.Current()
	if err == nil {
		userName = current.Username
	}

	logger = New("", "", INFO)
	//flag.BoolVar(&logger.toStderr, "logtostderr", false, "log to standard error")
	flag.BoolVar(&logger.toLoggly, "logtologgly", false, "log to loggly")
	flag.BoolVar(&logger.toLogglya, "logtologglya", false, "log to loggly")
	//flag.StringVar(&logger.Env, "env", "development", "set environment")
	var l uint
	flag.UintVar(&l, "loglevel", uint(INFO), "set logging level 0=Debug 1=Info 2=Error 3=Warning 4=Fatal")
	switch l {
	case 0:
		logger.level = DEBUG
	case 1:
		logger.level = INFO
	case 2:
		logger.level = ERROR
	case 3:
		logger.level = WARNING
	case 4:
		logger.level = FATAL
	default:
		logger.level = INFO
	}
	//flag.Parse()

	if logger.toStderr {
		logger.SetLogger("stderr")
	}
	if logger.toLoggly || logger.toLogglya {
		logger.SetLogger("loggly")
	}
	fmt.Printf("%#v", logger)
}

// iso8601 returns a formatted string in iso8601 format.
func iso8601(t time.Time) string {
	return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d.%03dZ",
		t.Year(),
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		t.Nanosecond()/100000)
}

// New creates a new instance of Log that will log to the provided io.Writer only if the method used
// for logging is enabled for the provided level. See package documentation for more details and examples.
func New(appName, env string, level Level) *Log {
	return &Log{
		Host:    host,
		App:     program,
		Env:     env,
		Loggers: map[string]Sender{},
		level:   level,
	}
}

// Send a log event to the console.
func (c *Console) Send(severity string, data interface{}) (err error) {
	c.m.Lock()
	defer c.m.Unlock()
	if d, ok := data.(string); ok {
		_, err = fmt.Fprintf(c.w, d)
	}
	return
}

// Send a log event to loggly.
func (l *Loggly) Send(severity string, data interface{}) error {
	p := &LogglyPost{
		Timestamp: iso8601(timeNow().UTC()),
		Env:       l.Env,
		App:       l.App,
		Host:      l.Host,
		Caller:    getCallersName(4),
		Pid:       pid,
		Level:     severity,
	}
	switch data.(type) {
	case string:
		p.Msg = map[string]interface{}{"str": data}
	case []interface{}:
		m, _ := data.([]interface{})
		if len(m) == 1 {
			switch m[0].(type) {
			case string:
				p.Msg = map[string]interface{}{"str": m[0]}
			case int, int32, int64, uint, uint8, uint32, uint64:
				p.Msg = map[string]interface{}{"int": m[0]}
			case float32:
				p.Msg = map[string]interface{}{"float": m[0].(float32)}
			case float64:
				p.Msg = map[string]interface{}{"float": m[0].(float64)}
			case map[string]interface{}:
				p.Msg = m[0]
			default:
				p.Msg = map[string]interface{}{"interface": m[0]}
			}
		} else {
			p.Msg = map[string]interface{}{"str": fmt.Sprint(m...)}
		}
	case map[string]interface{}:
		p.Msg = data
	}

	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	// Only send production and staging events to loggly
	// If not defined send to stderr
	if _, ok := logglyEnvironments[l.Env]; !ok {
		fmt.Fprintf(os.Stderr, string(b)+"\n")
		return nil
	}

	req, err := http.NewRequest("POST", l.url, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("%d %s", resp.StatusCode, body)
	}

	return nil
}

// SetLevel changes the logging level for the log instance.
func SetLevel(lvl Level) {
	logger.SetLevel(lvl)
}

// SetLevel changes the logging level for the log instance.
func (l *Log) SetLevel(lvl Level) {
	l.level = lvl
}

// SetEnv changes the logging environment.
func SetEnv(env string) {
	logger.SetEnv(env)
}

// SetEnv changes the logging environment.
func (l *Log) SetEnv(env string) {
	l.Env = env
}

// SetLogger defines which logger to use.
func SetLogger(logType string) {
	logger.SetLogger(logType)
}

// SetLogger defines which logger to use.
func (l *Log) SetLogger(logType string) {
	switch logType {
	case "loggly":
		l.Loggers[logType] = &Loggly{
			App:    l.App,
			Env:    l.Env,
			Client: &http.Client{},
			url:    logglyUrl,
		}
	case "stderr":
		l.Loggers[logType] = &Console{
			App: l.App,
			Env: l.Env,
			w:   os.Stderr,
			m:   &sync.Mutex{},
		}
	case "stdout":
		l.Loggers[logType] = &Console{
			App: l.App,
			Env: l.Env,
			w:   os.Stdout,
			m:   &sync.Mutex{},
		}
	case "file":
		// TODO implement
	}
}

func Debug(msg ...interface{}) error                   { return logger.Debug(msg...) }
func Debugf(fmtStr string, msg ...interface{}) error   { return logger.Debugf(fmtStr, msg...) }
func Info(msg ...interface{}) error                    { return logger.Info(msg...) }
func Infof(fmtStr string, msg ...interface{}) error    { return logger.Infof(fmtStr, msg...) }
func Error(msg ...interface{}) error                   { return logger.Error(msg...) }
func Errorf(fmtStr string, msg ...interface{}) error   { return logger.Errorf(fmtStr, msg...) }
func Warning(msg ...interface{}) error                 { return logger.Warning(msg...) }
func Warningf(fmtStr string, msg ...interface{}) error { return logger.Warningf(fmtStr, msg...) }
func Fatal(msg ...interface{})                         { logger.Fatal(msg...) }
func Fatalf(fmtStr string, msg ...interface{})         { logger.Fatalf(fmtStr, msg...) }

func (l *Log) Debug(msg ...interface{}) error                 { return l.log(DEBUG, msg...) }
func (l *Log) Debugf(fmtStr string, msg ...interface{}) error { return l.logf(DEBUG, fmtStr, msg...) }
func (l *Log) Info(msg ...interface{}) error                  { return l.log(INFO, msg...) }
func (l *Log) Infof(fmtStr string, msg ...interface{}) error  { return l.logf(DEBUG, fmtStr, msg...) }
func (l *Log) Error(msg ...interface{}) error                 { return l.log(ERROR, msg...) }
func (l *Log) Errorf(fmtStr string, msg ...interface{}) error { return l.logf(ERROR, fmtStr, msg...) }
func (l *Log) Warning(msg ...interface{}) error               { return l.log(WARNING, msg...) }
func (l *Log) Warningf(fmtStr string, msg ...interface{}) error {
	return l.logf(WARNING, fmtStr, msg...)
}

func (l *Log) Fatal(msg ...interface{}) {
	l.log(ERROR, msg...)
	os.Exit(1)
}

func (l *Log) Fatalf(fmtStr string, msg ...interface{}) {
	l.logf(ERROR, fmtStr, msg...)
	os.Exit(1)
}

// header generates a formated log header
//				L                A single character, representing the log level (eg 'I' for INFO)
//        time             iso8601
//        p                pid
//        file             The file name
//        line             The line number
//        funciton         The calling function
//        msg              The user-supplied message
func header(severity string, depth int) string {
	now := timeNow()

	h := fmt.Sprintf("%s%d %s %s] ",
		severity,
		pid,
		iso8601(now),
		getCallersName(depth),
	)

	return h
}

// log is called by all the other leveled logging functions.
func (l *Log) log(level Level, msg ...interface{}) error {
	if l.level > level {
		return nil
	}
	return l.send(level, "", msg...)
}

// logf is called by all the other leveled formatted logging functions.
func (l *Log) logf(level Level, fmtStr string, msg ...interface{}) error {
	if l.level > level {
		return nil
	}
	return l.send(level, fmtStr, msg...)
}

// send performs the request to the set loggers.
func (l *Log) send(level Level, fmtStr string, msg ...interface{}) error {
	severity := string(severityChars[level])
	for logType, logger := range l.Loggers {
		switch logType {
		case "loggly":
			if l.toLogglya {
				go func(s Sender) {
					var err error
					if fmtStr != "" {
						err = s.Send(severity, fmt.Sprintf(fmtStr, msg...))
					} else {
						err = s.Send(severity, msg)
					}
					if err != nil {
						fmt.Fprintf(os.Stderr, err.Error())
					}
				}(logger)
			} else {
				var err error
				if fmtStr != "" {
					err = logger.Send(severity, fmt.Sprintf(fmtStr, msg...))
				} else {
					err = logger.Send(severity, msg)
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, err.Error())
				}
			}
		default:
			var err error
			h := header(severity, 4)
			// stderr and stdout
			if fmtStr == "" {
				err = logger.Send(severity, h+fmt.Sprint(msg...)+"\n")
			} else {
				err = logger.Send(severity, fmt.Sprintf(h+fmtStr+"\n", msg...))
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, err.Error())
			}
		}
	}

	return nil
}

// Returns a string identifying a function on the call stack.
// Use depth=1 for the caller of the function that calls getCallersName, etc.
func getCallersName(depth int) string {
	pc, file, line, ok := runtime.Caller(depth + 1)
	if !ok {
		return "???"
	}

	fnname := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		fnname = fn.Name()
	}

	return fmt.Sprintf("%s:%d:%s", lastComponent(file), line, lastComponent(fnname))
}

// lastComponent
func lastComponent(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		path = path[index+1:]
	} else if index = strings.LastIndex(path, "\\"); index >= 0 {
		path = path[index+1:]
	}
	return path
}
