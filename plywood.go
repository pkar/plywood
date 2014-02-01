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

// Definitions of available levels: Debug < Info < Warning < Error.
const (
	DEBUG uint = iota
	INFO
	WARNING
	ERROR
	FATAL
)

const (
	logglyUrl = "https://logs-01.loggly.com/inputs/apikey/"
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
	Send(string, string, interface{}) error
}

// Console implements sender and logs events to the console.
type Console struct {
	w io.Writer
	m *sync.Mutex
}

// Loggly contains the meta for sending log events to loggly.
// Loggly implements sender.
type Loggly struct {
	Client *http.Client
	url    string
}

// Log contains the set loggers. Log output will be sent to
// and Log.Loggers defined (loggly, stderr, stdout)
type Log struct {
	Host               string
	App                string
	Env                string
	Loggers            map[string]Sender
	level              uint
	toStderr           bool
	toStdout           bool
	toLoggly           bool
	toLogglya          bool // async loggly posts
	timeTrackThreshold float64
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
	flag.BoolVar(&logger.toStderr, "plytostderr", false, "log to standard error")
	flag.BoolVar(&logger.toStdout, "plytostdout", false, "log to standard out")
	flag.BoolVar(&logger.toLoggly, "plytologgly", false, "log to loggly")
	flag.BoolVar(&logger.toLogglya, "plytologglya", false, "log to loggly async")
	flag.StringVar(&logger.Env, "plyenv", "development", "set environment")
	flag.Float64Var(&logger.timeTrackThreshold, "plytimethresh", 50.0, "set threshold for time track events")
	flag.UintVar(&logger.level, "plylevel", INFO, "set logging level 0=Debug 1=Info 2=Error 3=Warning 4=Fatal")

	// create all loggers and set their environments.
	logger.SetLogger("stderr")
	logger.SetLogger("stdout")
	logger.SetLogger("loggly")
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
func New(appName, env string, level uint) *Log {
	return &Log{
		Host:    host,
		App:     program,
		Env:     env,
		Loggers: map[string]Sender{},
		level:   level,
	}
}

// Send a log event to the console.
func (c *Console) Send(severity, env string, data interface{}) (err error) {
	c.m.Lock()
	defer c.m.Unlock()
	if d, ok := data.(string); ok {
		_, err = fmt.Fprintf(c.w, d)
	}
	return
}

// Send a log event to loggly.
func (l *Loggly) Send(severity, env string, data interface{}) error {
	p := &LogglyPost{
		Timestamp: iso8601(timeNow().UTC()),
		Env:       env,
		App:       program,
		Host:      host,
		Caller:    getCallersName(5),
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
		fmt.Fprintf(os.Stderr, "E "+err.Error()+"] \n")
		return err
	}

	// Only send production and staging events to loggly
	// If not defined send to stderr
	if _, ok := logglyEnvironments[env]; !ok {
		fmt.Fprintf(os.Stderr, "E "+"env not set: "+env+"] "+string(b)+"\n")
		return nil
	}

	req, err := http.NewRequest("POST", l.url, strings.NewReader(string(b)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "E "+err.Error()+"] "+string(b)+"\n")
		return err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "E "+err.Error()+"] "+string(b)+"\n")
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "E "+err.Error()+"] "+string(b)+"\n")
		return err
	}

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "E "+resp.Status+"] "+string(b)+"\n")
		return fmt.Errorf("%d %s", resp.StatusCode, body)
	}

	return nil
}

// TimeTrack is a helper to get function times
// usage: defer log.TimeTrack(time.Now())
func TimeTrack(start time.Time, name interface{}) {
	logger.TimeTrack(start, name)
}

// TimeTrack is a helper to get function times
// usage: defer log.TimeTrack(time.Now(), "functionName")
func (l *Log) TimeTrack(start time.Time, name interface{}) {
	elapsed := time.Since(start)
	ms := float64(elapsed) / float64(time.Millisecond)
	if ms > l.timeTrackThreshold {
		logger.Info(map[string]interface{}{
			"time": map[string]interface{}{
				"name": name,
				"ms":   ms,
			},
		})
	}
}

// DebugLogger just prints out the current state of the logger.
func DebugLogger() {
	fmt.Fprintf(os.Stderr, "%#v\n", logger)
}

// DebugLogger just prints out the current state of the logger.
func (l *Log) DebugLogger() {
	fmt.Fprintf(os.Stderr, "%#v", l)
}

// SetLevel changes the logging level for the log instance.
func SetLevel(lvl uint) {
	logger.SetLevel(lvl)
}

// SetLevel changes the logging level for the log instance.
func (l *Log) SetLevel(lvl uint) {
	l.level = lvl
}

// SetTimeTrackThreshold logs only events timed higher.
func SetTimeTrackThreshold(t float64) {
	logger.SetTimeTrackThreshold(t)
}

// SetTimeTrackThreshold logs only events timed higher.
func (l *Log) SetTimeTrackThreshold(t float64) {
	l.timeTrackThreshold = t
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
			Client: &http.Client{},
			url:    logglyUrl + "tag/" + program,
		}
	case "stderr":
		l.Loggers[logType] = &Console{
			w: os.Stderr,
			m: &sync.Mutex{},
		}
	case "stdout":
		l.Loggers[logType] = &Console{
			w: os.Stdout,
			m: &sync.Mutex{},
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
func (l *Log) Infof(fmtStr string, msg ...interface{}) error  { return l.logf(INFO, fmtStr, msg...) }
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
func (l *Log) log(level uint, msg ...interface{}) error {
	if l.level > level {
		return nil
	}
	return l.send(level, "", msg...)
}

// logf is called by all the other leveled formatted logging functions.
func (l *Log) logf(level uint, fmtStr string, msg ...interface{}) error {
	if l.level > level {
		return nil
	}
	return l.send(level, fmtStr, msg...)
}

// send performs the request to the set loggers.
func (l *Log) send(level uint, fmtStr string, msg ...interface{}) error {
	severity := string(severityChars[level])
	if l.toLogglya {
		go func(s Sender) {
			var err error
			if fmtStr != "" {
				err = s.Send(severity, l.Env, fmt.Sprintf(fmtStr, msg...))
			} else {
				err = s.Send(severity, l.Env, msg)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, err.Error())
			}
		}(l.Loggers["loggly"])
	}
	if l.toLoggly {
		var err error
		if fmtStr != "" {
			err = l.Loggers["loggly"].Send(severity, l.Env, fmt.Sprintf(fmtStr, msg...))
		} else {
			err = l.Loggers["loggly"].Send(severity, l.Env, msg)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
		}
	}
	if l.toStderr {
		var err error
		h := header(severity, 5)
		// stderr and stdout
		if fmtStr == "" {
			err = l.Loggers["stderr"].Send(severity, l.Env, h+fmt.Sprint(msg...)+"\n")
		} else {
			err = l.Loggers["stderr"].Send(severity, l.Env, fmt.Sprintf(h+fmtStr+"\n", msg...))
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
		}
	}
	if l.toStdout {
		var err error
		h := header(severity, 5)
		// stderr and stdout
		if fmtStr == "" {
			err = l.Loggers["stdout"].Send(severity, l.Env, h+fmt.Sprint(msg...)+"\n")
		} else {
			err = l.Loggers["stdout"].Send(severity, l.Env, fmt.Sprintf(h+fmtStr+"\n", msg...))
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
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
