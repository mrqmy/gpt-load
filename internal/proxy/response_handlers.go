package proxy

import (
	"io"
	"net/http"
	"strings"
	"time"

	"gpt-load/internal/jsonengine"
	"gpt-load/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (ps *ProxyServer) handleStreamingResponse(c *gin.Context, resp *http.Response, group *models.Group) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logrus.Error("Streaming unsupported by the writer, falling back to normal response")
		ps.handleNormalResponse(c, resp, group)
		return
	}

	// NOTE: 流式响应(SSE)格式为 "data: {...}\n\n"，不是纯 JSON
	// 出站规则暂不支持流式响应，仅支持普通 JSON 响应
	buf := make([]byte, 4*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				logUpstreamError("writing stream to client", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			logUpstreamError("reading from upstream", err)
			return
		}
	}
}

func (ps *ProxyServer) handleNormalResponse(c *gin.Context, resp *http.Response, group *models.Group) {
	start := time.Now()
	
	// 检查是否有出站规则且响应是 JSON
	if len(group.OutboundRuleList) > 0 {
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "json") {
			engine := jsonengine.New(group.OutboundRuleList)
			if err := engine.ProcessTo(resp.Body, c.Writer); err != nil {
				logUpstreamError("jsonengine processing", err)
			}
			logrus.WithField("duration", time.Since(start)).Debug("jsonengine processing completed")
			return
		}
	}

	// 无规则或非 JSON，使用大缓冲区直接透传
	buf := make([]byte, 1024*1024) // 1MB buffer
	total, err := io.CopyBuffer(c.Writer, resp.Body, buf)
	if err != nil {
		logUpstreamError("copying response body", err)
	}
	
	logrus.WithFields(logrus.Fields{
		"bytes":    total,
		"duration": time.Since(start),
	}).Debug("io.Copy completed")
}
