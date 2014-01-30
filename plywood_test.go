package plywood

import (
	//"bytes"
	//"fmt"
	//"io"
	//"os"
	"testing"
	"time"
)

var lg *Log

func init() {
	lg = New("test", "testing", INFO)
	lg.SetLogger("loggly")
	lg.SetLogger("stderr")
	// to send loggly events to loggly instead of stderr
	//logglyEnvironments["testing"] = true
}

func TestIso8601(t *testing.T) {
	iso := iso8601(time.Now().UTC())
	if iso == "" {
		t.Error("timestamp not returned")
	}
}

func TestGetCallersName(t *testing.T) {
	name := getCallersName(0)
	if name == "???" {
		t.Error("caller not returned")
	}
}

func TestHeader(t *testing.T) {
	h := header("I", 0)
	if h == "" {
		t.Error("header not returned")
	}
}

func TestSendString(t *testing.T) {
	lg.Error("hello", "bb")
	lg.Errorf("%s, %s", "hello", "aa")
}

func TestSendInt(t *testing.T) {
	lg.Error(123)
	lg.Errorf("%d, %d", 123, 456)
}

func TestSendFloat(t *testing.T) {
	lg.Error(123.1)
	lg.Errorf("%f, %f", 123.1, 456.03)
}

func TestSendMultipleStrings(t *testing.T) {
	lg.Error("a", "b", "c", "d")
	lg.Errorf("%s, %s", "a", "b")
}

func TestSendMultipleInterface(t *testing.T) {
	lg.Error("a", "b", 123, 456.99)
	lg.Errorf("%s, %s %d %f", "a", "b", 123, 456.449)
}

func TestSendMapStringInterface(t *testing.T) {
	msi := map[string]interface{}{
		"i": 123,
		"f": 123.77,
		"s": "ms",
	}
	// renders as json hash for loggly
	lg.Error(msi)
	// renders as string
	lg.Errorf("%v", msi)
}
