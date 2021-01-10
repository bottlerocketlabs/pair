package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stuart-warren/pair/pkg/logging"
)

func justIP(hostPort string) string {
	ip := hostPort
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

func firstIP(commaSepList string) string {
	ips := strings.Split(commaSepList, ", ")
	return justIP(ips[0])
}

func (s *server) LogrusLogHandler(h http.Handler) http.Handler {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(s.log.Writer())
	fn := func(w http.ResponseWriter, r *http.Request) {

		clientIP := justIP(r.RemoteAddr)
		forwardedIP := firstIP(r.Header.Get("X-Forwarded-For"))
		record := &logging.LogRecord{
			ResponseWriter: w,
			Status:         http.StatusOK,
		}
		messagePattern := "time=%s method=%s ip=%q fwd=%q proto=%s uri=%q state=%s"

		startTime := time.Now()
		connectLog := logger.WithFields(logrus.Fields{
			"ip":           clientIP,
			"fwd":          forwardedIP,
			"timestamp":    int(startTime.UTC().UnixNano() / int64(time.Millisecond)),
			"method":       r.Method,
			"uri":          r.RequestURI,
			"proto":        r.Proto,
			"state":        "connected",
			"referer":      r.Referer(),
			"user-agent":   r.UserAgent(),
			"content-type": r.Header.Get("Content-Type"),
		})
		connectLog.Info(fmt.Sprintf(messagePattern, startTime.UTC().Format(time.RFC3339), r.Method, clientIP, forwardedIP, r.Proto, r.RequestURI, "connected"))

		h.ServeHTTP(record, r)
		finishTime := time.Now()
		connectLog.WithFields(logrus.Fields{
			"status":       record.Status,
			"timestamp":    int(finishTime.UTC().UnixNano() / int64(time.Millisecond)),
			"size":         record.ResponseBytes,
			"duration":     finishTime.Sub(startTime).Milliseconds(),
			"state":        "disconnected",
			"content-type": record.ContentType,
		}).Info(fmt.Sprintf(messagePattern, finishTime.UTC().Format(time.RFC3339), r.Method, clientIP, forwardedIP, r.Proto, r.RequestURI, "disconnected"))

	}
	return http.HandlerFunc(fn)
}

func (s *server) ApacheLogHandler(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
			clientIP = clientIP[:colon]
		}

		record := &logging.ApacheLogRecord{
			LogRecord: &logging.LogRecord{
				ResponseWriter: w,
				Status:         http.StatusOK,
			},
			IP:          clientIP,
			Time:        time.Time{},
			Method:      r.Method,
			URI:         r.RequestURI,
			Protocol:    r.Proto,
			ElapsedTime: time.Duration(0),
		}

		startTime := time.Now()
		connect := &logging.ConnectLogRecord{
			IP:       clientIP,
			Time:     startTime,
			Method:   r.Method,
			URI:      r.RequestURI,
			Protocol: r.Proto,
		}
		connect.Log(s.log.Writer())
		h.ServeHTTP(record, r)
		finishTime := time.Now()

		record.Time = finishTime.UTC()
		record.ElapsedTime = finishTime.Sub(startTime)

		record.Log(s.log.Writer())
	}
	return http.HandlerFunc(fn)
}
