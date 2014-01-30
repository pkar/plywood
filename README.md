# plywood

A mix of the various logging packages with the addition of loggly
as an option. There are two options, stderr and loggly, supervisord can 
handle writing to file from stderr and rotating if wanted.

loggly posts are done in goroutines, writing to stderr is not optimized, more for development.
loggly only posts in production env

### Loggly
set hardcoded logglyUrl at the top of plywood.go to your loggly api key url

### Running
```go
# -logtologglya is async requests to loggly in seperate goroutines
./myapp -env=production -logtostderr -logtologglya -loglevel=1
```

### Example
```go
import (
	"flag"
	log "plywood"
)

func main() {
	//log.SetEnv("production")
	//log.SetLogger("loggly")
	//log.SetLogger("stderr")

	log.Debug("some debug")
	log.Info("some info")
	log.Error("some error")
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
