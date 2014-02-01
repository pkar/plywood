# plywood

A mix of the various logging packages with the addition of loggly
as an option. There are two options, stderr and loggly, supervisord can 
handle writing to file from stderr and rotating if wanted.

loggly posts are done in goroutines, writing to stderr is not optimized, more for development.
loggly only posts in production env

### Loggly
Update the hardcoded api key in loggly.go
API endpoint: "https://logs-01.loggly.com/inputs/apikey/"

### Running
```go
# -plytologglya is async requests to loggly in seperate goroutines -plytologgly for sync request testing
./myapp -plyenv=production -plytostderr -plytologglya -plylevel=1 -plytimethresh=100.0
```

### Example
```go
import (
	"time"

	log "github.ngmoco.com/Eurisko/plywood"
)

func main() {
	defer log.TimeTrack(time.Now(), "some key")
	//log.SetEnv("production")
	//log.SetLogger("loggly")
	//log.SetLogger("stderr")

	log.Debug("some debug")
	log.Info("some info")
	log.Error("some error")
	log.Error(map[string]interface{}{"custom": 12.1})
	log.Errorf("some error %s", err)
	log.Warning("some warning")
	log.Fatal("fatal")
}
```

### See other wood makers

```
github.com/golang/glog
github.com/ngmoco/timber
github.com/divoxx/llog
github.com/couchbaselabs/logg
```
