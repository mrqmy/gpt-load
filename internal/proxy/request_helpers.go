package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"time"

	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/jsonengine"
	"gpt-load/internal/models"

	"github.com/sirupsen/logrus"
)

func (ps *ProxyServer) applyParamOverrides(bodyBytes []byte, group *models.Group) ([]byte, error) {
	if len(group.ParamOverrides) == 0 || len(bodyBytes) == 0 {
		return bodyBytes, nil
	}

	var requestData map[string]any
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		logrus.Warnf("failed to unmarshal request body for param override, passing through: %v", err)
		return bodyBytes, nil
	}

	for key, value := range group.ParamOverrides {
		requestData[key] = value
	}

	return json.Marshal(requestData)
}

// applyInboundRules applies JSON transformation rules to request body
func (ps *ProxyServer) applyInboundRules(bodyBytes []byte, group *models.Group) ([]byte, error) {
	if len(group.InboundRuleList) == 0 || len(bodyBytes) == 0 {
		return bodyBytes, nil
	}

	start := time.Now()

	// 记录引擎创建开始时间
	engineCreateStart := time.Now()
	engine, err := jsonengine.NewPathEngine(group.InboundRuleList)
	engineCreateDuration := time.Since(engineCreateStart)

	if err != nil {
		logrus.WithError(err).WithField("group_name", group.Name).Warn("Failed to create path engine for inbound rules")
		return bodyBytes, nil // 失败时返回原始数据
	}

	// 记录处理开始时间
	processStart := time.Now()
	var buf bytes.Buffer
	if err := engine.Process(bytes.NewReader(bodyBytes), &buf); err != nil {
		logrus.WithError(err).WithField("group_name", group.Name).Warn("Failed to apply inbound rules")
		return bodyBytes, nil // 失败时返回原始数据
	}
	processDuration := time.Since(processStart)
	totalDuration := time.Since(start)

	// 详细性能日志
	logrus.WithFields(logrus.Fields{
		"group":                  group.Name,
		"rule_count":             len(group.InboundRuleList),
		"input_bytes":            len(bodyBytes),
		"output_bytes":           buf.Len(),
		"engine_create_ms":       engineCreateDuration.Milliseconds(),
		"process_ms":             processDuration.Milliseconds(),
		"total_ms":               totalDuration.Milliseconds(),
		"engine_create_seconds":  engineCreateDuration.Seconds(),
		"process_seconds":        processDuration.Seconds(),
		"total_seconds":          totalDuration.Seconds(),
	}).Debugf("Inbound PathEngine processing: create=%v, process=%v, total=%v",
		engineCreateDuration, processDuration, totalDuration)

	return buf.Bytes(), nil
}

// logUpstreamError provides a centralized way to log errors from upstream interactions.
func logUpstreamError(context string, err error) {
	if err == nil {
		return
	}
	if app_errors.IsIgnorableError(err) {
		logrus.Debugf("Ignorable upstream error in %s: %v", context, err)
	} else {
		logrus.Errorf("Upstream error in %s: %v", context, err)
	}
}

// handleGzipCompression checks for gzip encoding and decompresses the body if necessary.
func handleGzipCompression(resp *http.Response, bodyBytes []byte) []byte {
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, gzipErr := gzip.NewReader(bytes.NewReader(bodyBytes))
		if gzipErr != nil {
			logrus.Warnf("Failed to create gzip reader for error body: %v", gzipErr)
			return bodyBytes
		}
		defer reader.Close()

		decompressedBody, readAllErr := io.ReadAll(reader)
		if readAllErr != nil {
			logrus.Warnf("Failed to decompress gzip error body: %v", readAllErr)
			return bodyBytes
		}
		return decompressedBody
	}
	return bodyBytes
}
