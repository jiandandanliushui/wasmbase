package main

import (
	"runtime"
	"time"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

const tickMilliseconds uint32 = 100

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

type vmContext struct {
	// Embed the default VM context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultVMContext
}

// Override types.DefaultVMContext.
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{}
}

type pluginContext struct {
	// Embed the default plugin context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultPluginContext
	// the remaining token for rate limiting, refreshed periodically.
	remainToken int
	// // the preconfigured request per second for rate limiting.
	// requestPerSecond int
	// NOTE(jianfeih): any concerns about the threading and mutex usage for tinygo wasm?
	// the last time the token is refilled with `requestPerSecond`.
	lastRefillNanoSec int64

	contextID uint32

	callBack func(numHeaders, bodySize, numTrailers int)
	cnt      int
}

// Override types.DefaultPluginContext.
func (p *pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &httpHeaders{contextID: contextID, pluginContext: p}
}

type httpHeaders struct {
	// Embed the default http context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultHttpContext
	contextID     uint32
	pluginContext *pluginContext
}

// Additional headers supposed to be injected to response headers.
var additionalHeaders = map[string]string{
	"who-am-i":    "wasm-extension",
	"injected-by": "istio-api!",
}

func (ctx *httpHeaders) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	for key, value := range additionalHeaders {
		proxywasm.AddHttpResponseHeader(key, value)
	}
	return types.ActionContinue
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) OnPluginStart(pluginConfigurationSize int) types.OnPluginStartStatus {
	if err := proxywasm.SetTickPeriodMilliSeconds(tickMilliseconds); err != nil {
		proxywasm.LogCriticalf("failed to set tick period: %v", err)
		return types.OnPluginStartStatusFailed
	}
	proxywasm.LogCriticalf("set tick period milliseconds: %d", tickMilliseconds)
	ctx.callBack = func(numHeaders, bodySize, numTrailers int) {
		ctx.cnt++
		proxywasm.LogCriticalf("called %d for contextID=%d", ctx.cnt, ctx.contextID)
		headers, err := proxywasm.GetHttpCallResponseHeaders()
		if err != nil && err != types.ErrorStatusNotFound {
			panic(err)
		}
		for _, h := range headers {
			proxywasm.LogCriticalf("response header for the dispatched call: %s: %s", h[0], h[1])
		}

		headers, err = proxywasm.GetHttpCallResponseTrailers()
		if err != nil && err != types.ErrorStatusNotFound {
			panic(err)
		}
		for _, h := range headers {
			proxywasm.LogCriticalf("response trailer for the dispatched call: %s: %s", h[0], h[1])
		}

		b, err := proxywasm.GetHttpCallResponseBody(0, bodySize)
		if err != nil {
			proxywasm.LogCriticalf("failed to get response body: %v", err)
			proxywasm.ResumeHttpRequest()
			return
		}
		proxywasm.LogCriticalf("为什么下面这句没有输出？？？")
		proxywasm.LogCriticalf("response body: %s", string(b))

	}
	return types.OnPluginStartStatusOK
}

func (ctx *httpHeaders) OnHttpRequestHeaders(int, bool) types.Action {

	runtime.GOMAXPROCS(2)
	current := time.Now().UnixNano()
	if current > ctx.pluginContext.lastRefillNanoSec+1e9 {
		ctx.pluginContext.remainToken = 2
		ctx.pluginContext.lastRefillNanoSec = current
	}

	proxywasm.LogCriticalf("Current time %v, last refill time %v, the remain token %v",
		current, ctx.pluginContext.lastRefillNanoSec, ctx.pluginContext.remainToken)

	// //log there is too much rate right now
	// proxywasm.LogCritical("There is too much rate now\n")

	// hs, err := proxywasm.GetHttpRequestHeaders()
	// if err != nil {
	// 	proxywasm.LogCriticalf("failed to get request headers: %v", err)
	// 	return types.ActionContinue
	// }
	// for _, h := range hs {
	// 	proxywasm.LogCriticalf("request header: %s: %s", h[0], h[1])
	// }
	// cluster := "details.default.svc.cluster.local"
	// proxywasm.LogCriticalf("这次一定成功！")
	// requestheaders := [][2]string{
	// 	{":authority", "details.default:9080"},
	// 	{":path", "/details/0"},
	// 	{":method", "GET"},
	// 	{":scheme", "http"},
	// 	{":accept", "*/*"},
	// }

	// if _, err := proxywasm.DispatchHttpCall(cluster, requestheaders, nil, nil,
	// 	50000, ctx.callBack); err != nil {
	// 	proxywasm.LogCriticalf("dipatch httpcall failed: %v", err)
	// 	return types.ActionContinue
	// }

	return types.ActionContinue
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) OnTick() {
	proxywasm.LogCriticalf("开始执行回调函数3")
	headers := [][2]string{
		{":method", "POST"}, {":authority", "details:9080"}, {"accept", "*/*"}, {":path", "/details/0"},
	}
	// // Pick random value to select the request path.
	// buf := make([]byte, 1)
	// _, _ = rand.Read(buf)
	// if buf[0]%2 == 0 {
	// 	headers = append(headers, [2]string{":path", "/ok"})
	// } else {
	// 	headers = append(headers, [2]string{":path", "/fail"})
	// }
	if _, err := proxywasm.DispatchHttpCall("outbound|9080||details.default.svc.cluster.local", headers, nil, nil, 5000, ctx.callBack); err != nil {
		proxywasm.LogCriticalf("dispatch httpcall failed: %v", err)
	}
}
