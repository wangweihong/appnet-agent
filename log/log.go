package log

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/astaxie/beego/logs"
)

var (
	Logger *logs.BeeLogger
)

/*
func Debug(format string, v ...interface{}) {
	logHandler.Debug(format, v...)
}

func Info(format string, v ...interface{}) {
	logHandler.Info(format, v...)
}

func Trace(format string, v ...interface{}) {
	logHandler.Trace(format, v...)
}

func Warn(format string, v ...interface{}) {
	logHandler.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	logHandler.Error(format, v...)
}

func Critical(format string, v ...interface{}) {
	logHandler.Critical(format, v...)
}

func Emergency(format string, v ...interface{}) {
	logHandler.Emergency(format, v...)
}
*/

func initLogger(logfile string) error {

	fileCreate := true
	if err := os.MkdirAll(filepath.Dir(logfile), 0755); err != nil {
		if !os.IsExist(err) {
			fileCreate = false
		} else {
			if _, err := os.Create(logfile); err != nil {
				if !os.IsExist(err) {
					fileCreate = false
				}
			}
		}
	}

	if !fileCreate {
		fmt.Println("logfile fail")
		return fmt.Errorf("can't create log file")
	}

	log := logs.NewLogger(100000)
	log.SetLogger("console", "")
	log.SetLevel(logs.LevelDebug)
	log.EnableFuncCallDepth(true)

	jsonConfig := fmt.Sprintf("{\"filename\":\"%s\",\"maxlines\":5000,\"maxsize\":10240000}", logfile)
	log.SetLogger("file", jsonConfig)
	Logger = log
	return nil
}

func init() {
	err := initLogger("./agent.log")
	if err != nil {
		panic(err)
	}
}
