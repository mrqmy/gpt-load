package jsonengine

import "sync"

const (
	// DefaultPositionsCap SIMD 扫描位置缓冲区默认容量
	// 512KB / 4 (平均每 4 字节一个结构字符) = 128K
	DefaultPositionsCap = 128 * 1024

	// DefaultPathStackCap 路径栈默认容量
	DefaultPathStackCap = 32

	// DefaultKeyBufferCap key 缓冲区默认容量
	DefaultKeyBufferCap = 256
)

// PathProcessorPool 路径处理器对象池
var PathProcessorPool = sync.Pool{
	New: func() interface{} {
		return &PathProcessor{
			positions:  make([]uint32, DefaultPositionsCap),
			pathStack:  make([]pathEntry, 0, DefaultPathStackCap),
			keyBuffer:  make([]byte, 0, DefaultKeyBufferCap),
			outputBuf:  make([]byte, 0, 4096),
		}
	},
}

// GetPathProcessor 从池中获取处理器
func GetPathProcessor(matcher *PathMatcher) *PathProcessor {
	p := PathProcessorPool.Get().(*PathProcessor)
	p.matcher = matcher
	
	// ⚡ 性能优化：检查是否有 Add 规则（只在初始化时检查一次）
	p.hasAddRules = false
	if matcher != nil {
		for _, rule := range matcher.rules {
			if rule.Action == ActionAdd {
				p.hasAddRules = true
				break
			}
		}
	}
	
	p.Reset()
	return p
}

// PutPathProcessor 归还处理器到池中
func PutPathProcessor(p *PathProcessor) {
	if p == nil {
		return
	}
	p.matcher = nil
	// 清理可能的大缓冲区引用
	p.pathStack = p.pathStack[:0]
	p.keyBuffer = p.keyBuffer[:0]
	p.outputBuf = p.outputBuf[:0]
	PathProcessorPool.Put(p)
}

// bytesPool 字节切片池（用于临时缓冲）
var bytesPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 64*1024) // 64KB
		return &buf
	},
}

// GetBytes 获取临时字节缓冲区
func GetBytes() *[]byte {
	return bytesPool.Get().(*[]byte)
}

// PutBytes 归还字节缓冲区
func PutBytes(buf *[]byte) {
	if buf == nil {
		return
	}
	bytesPool.Put(buf)
}
