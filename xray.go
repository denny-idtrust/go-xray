package mid_go

import (
	"bytes"
	"github.com/aws/aws-xray-sdk-go/header"
	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func XRayMiddleware(sn xray.SegmentNamer) gin.HandlerFunc {
	return func(c *gin.Context) {
		w := &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = w
		var name string
		if sn != nil {
			name = sn.Name(c.Request.Host)
		} else {
			name = os.Getenv("XRAY_NAME")
		}
		traceId := c.Request.Header.Get(os.Getenv("XRAY_TRACE"))

		traceHeader := header.FromString(traceId)

		ctx, seg := xray.NewSegmentFromHeader(c.Request.Context(), name, c.Request, traceHeader)
		logTrx := NewTransactionContext(seg.TraceID)
		log := logTrx.LogContext
		log.Info(os.Getenv("XRAY_TRACE"), ": ", traceId)
		c.Request = c.Request.WithContext(ctx)

		captureRequestData(c, seg)
		c.Next()
		captureResponseData(c, seg)
		if err := seg.AddMetadata("response", w.body.String()); err != nil {
			log.Error("Error adding metadata to segment")
		}
		log.Info("Trace ID:", seg.TraceID)
		log.Info("Segment Name:", seg.Name)
		log.Debug("Start Time:", seg.StartTime)
		log.Debug("End Time:", seg.EndTime)
		log.Debug("Metadata:", seg.Metadata)

		seg.Close(nil)
		// Build
	}

}

// CaptureRequestData Write request data to segment
func captureRequestData(c *gin.Context, seg *xray.Segment) {
	r := c.Request
	seg.Lock()
	defer seg.Unlock()
	segmentRequest := seg.GetHTTP().GetRequest()
	segmentRequest.Method = r.Method
	segmentRequest.URL = r.URL.String()
	segmentRequest.XForwardedFor = hasXForwardedFor(r)
	segmentRequest.ClientIP = clientIP(r)
	segmentRequest.UserAgent = r.UserAgent()
	c.Writer.Header().Set(os.Getenv("XRAY_TRACE"), createTraceHeader(r, seg))
}

func captureRequestDataNet(r *http.Request, seg *xray.Segment) {
	seg.Lock()
	defer seg.Unlock()
	segmentRequest := seg.GetHTTP().GetRequest()
	segmentRequest.Method = r.Method
	segmentRequest.URL = r.URL.String()
	segmentRequest.XForwardedFor = hasXForwardedFor(r)
	segmentRequest.ClientIP = clientIP(r)
	segmentRequest.UserAgent = r.UserAgent()
}

// LoggingRoundTripper This type implements the http.RoundTripper interface
type LoggingRoundTripper struct {
	R   http.RoundTripper
	Seg *xray.Segment
}

func (lrt LoggingRoundTripper) RoundTrip(req *http.Request) (res *http.Response, e error) {
	logTrx := NewTransactionContext(lrt.Seg.TraceID)
	log := logTrx.LogContext
	// Do "before sending requests" actions here.
	log.Info("Sending request to ", req.URL)
	captureRequestDataNet(req, lrt.Seg)
	// Send the request, get the response (or the error)
	res, e = lrt.R.RoundTrip(req)
	captureResponseDataNet(res, lrt.Seg)
	// Handle the result.
	if e != nil {
		log.Error("Error: ", e)
	} else {
		log.Error("Received response ", res.Status)
	}
	defer lrt.Seg.Close(nil)

	return
}

// Write response data to segment
func captureResponseData(c *gin.Context, seg *xray.Segment) {
	respStatus := c.Writer.Status()

	seg.Lock()
	defer seg.Unlock()
	seg.GetHTTP().GetResponse().Status = respStatus
	seg.GetHTTP().GetResponse().ContentLength = c.Writer.Size()

	if respStatus >= 400 && respStatus < 500 {
		seg.Error = true
	}
	if respStatus == 429 {
		seg.Throttle = true
	}
	if respStatus >= 500 && respStatus < 600 {
		seg.Fault = true
	}
}

func captureResponseDataNet(c *http.Response, seg *xray.Segment) {
	respStatus := c.StatusCode

	seg.Lock()
	defer seg.Unlock()
	seg.GetHTTP().GetResponse().Status = respStatus
	seg.GetHTTP().GetResponse().ContentLength = int(c.ContentLength)

	if respStatus >= 400 && respStatus < 500 {
		seg.Error = true
	}
	if respStatus == 429 {
		seg.Throttle = true
	}
	if respStatus >= 500 && respStatus < 600 {
		seg.Fault = true
	}
}

// Add tracing data to header
func createTraceHeader(r *http.Request, seg *xray.Segment) string {
	trace := parseHeaders(r.Header)
	if trace["Root"] != "" {
		seg.TraceID = trace["Root"]
		seg.RequestWasTraced = true
	}
	if trace["Parent"] != "" {
		seg.ParentID = trace["Parent"]
	}
	// Don't use the segment's header here as we only want to
	// send back the root and possibly sampled values.
	var respHeader bytes.Buffer
	respHeader.WriteString("Root=")
	respHeader.WriteString(seg.TraceID)

	seg.Sampled = trace["Sampled"] != "0"
	if trace["Sampled"] == "?" {
		respHeader.WriteString(";Sampled=")
		respHeader.WriteString(strconv.Itoa(btoi(seg.Sampled)))
	}
	return respHeader.String()
}

func hasXForwardedFor(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-For") != ""
}

func clientIP(r *http.Request) string {
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		return strings.TrimSpace(strings.Split(forwardedFor, ",")[0])
	}

	return r.RemoteAddr
}

func parseHeaders(h http.Header) map[string]string {
	m := map[string]string{}
	s := h.Get(os.Getenv("XRAY_TRACE"))
	for _, c := range strings.Split(s, ";") {
		p := strings.SplitN(c, "=", 2)
		k := strings.TrimSpace(p[0])
		v := ""
		if len(p) > 1 {
			v = strings.TrimSpace(p[1])
		}
		m[k] = v
	}
	return m
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
