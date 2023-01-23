package mid_go

import (
	"bytes"
	"fmt"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"os"
	"strconv"
	"strings"
	"time"
)

type LogFormat struct {
	TimestampFormat string
}

func NewLogging() {
	logUser := logrus.StandardLogger()
	formatter := LogFormat{}
	formatter.TimestampFormat = "02/01/2006 15:04:05,999"
	logUser.SetReportCaller(true)
	logUser.SetFormatter(&formatter)
	levelLog := os.Getenv("LOG_LEVEL")
	logrus.Info(levelLog)
	var lvEnum logrus.Level
	switch levelLog {
	case "DEBUG":
		lvEnum = logrus.DebugLevel
	case "TRACE":
		lvEnum = logrus.TraceLevel
	default:
		lvEnum = logrus.InfoLevel
	}
	logUser.SetLevel(lvEnum)
}

func (f LogFormat) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}
	file := entry.Caller.File

	b.WriteString(entry.Time.Format(f.TimestampFormat))
	b.WriteByte(' ')
	b.WriteString(strings.ToUpper(entry.Level.String()))
	b.WriteByte(' ')
	filename := file[strings.LastIndex(file, "/")+1:] + ":" + strconv.Itoa(entry.Caller.Line)
	//line := fmt.Sprintf("%d", entry.Caller.Line)
	b.WriteString(filename)
	//b.WriteString(line)
	b.WriteByte(' ')
	b.WriteString(os.Getenv("APP_NAME"))
	b.WriteByte(' ')

	var traceId, emailReq, mitraReq, responseTime string
	for key, value := range entry.Data {
		if key == "TRACEID" {
			traceId = fmt.Sprint(value)
		} else if key == "EMAILREQ" {
			emailReq = fmt.Sprint(value)
		} else if key == "MITRAREQ" {
			mitraReq = fmt.Sprint(value)
		} else if key == "STARTTIME" {
			p := message.NewPrinter(language.Indonesian)
			elapsedTime := time.Now().UnixMilli() - value.(int64)
			responseTime = " [" + p.Sprintf("%d", elapsedTime) + " ms]"
		}
	}

	if traceId != "" {
		b.WriteString(" TRACEID " + traceId + " ")
	}
	if emailReq != "" {
		b.WriteString("[" + emailReq)
		if mitraReq != "" {
			b.WriteString("|" + mitraReq)
		}
		b.WriteString("] ")
	}

	if entry.Message != "" {
		b.WriteString("MSSG:" + entry.Message)
	}
	if responseTime != "" {
		b.WriteString(responseTime)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

type TransactionContext struct {
	LogContext *logrus.Entry
}

func NewTransactionContext(traceId string) *TransactionContext {
	fields := logrus.Fields{"TRACEID": traceId, "STARTTIME": time.Now().UnixMilli()}
	return &TransactionContext{logrus.WithFields(fields)}
}
