package main

import (
	"bufio"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	log "github.com/cihub/seelog"
	"github.com/elazarl/goproxy"
)

type config struct {
	ListenAddr string   `toml:"listen_addr"`
	UrlMatches []string `toml:"url_matches"`
	Timeout    int      `toml:"time_out"`
	LogLevel   int      `toml:"log_level"`
	Verbose    bool     `toml:"proxy_verbose"`
	CDN        []string `toml:"cdn"`
}

var (
	timeout = 5 * time.Second //超时时间
	index   = make(chan int)  //获取CDN下标的队列
	add     = make(chan int)  //通知CDN下标加1的队列
	Config  config            //配置文件
)

func main() {
	defer log.Flush()
	defer func() {
		if err := recover(); err != nil {
			log.Critical(err)
			log.Critical(string(debug.Stack()))
		}
	}()
	if err := ReadConfig(); err != nil {
		log.Error(err)
		os.Exit(0)
		return
	}
	if len(Config.CDN) < 1 {
		log.Error("淘气了,CDN也不配置～～")
		os.Exit(0)
		return
	}

	proxy := goproxy.NewProxyHttpServer()

	proxy.Verbose = Config.Verbose
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.OnRequest(goproxy.ReqHostIs("kyfw.12306.cn:443")).HandleConnect(goproxy.AlwaysMitm)
	// proxy.OnRequest(goproxy.ReqHostIs("kyfw.12306.cn")).HandleConnect(goproxy.AlwaysMitm)

	// proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile("^.*kyfw\\.12306\\.cn$"))).HandleConnect(goproxy.AlwaysMitm)
	// proxy.OnRequest(goproxy.ReqHostIs("kyfw.12306.cn:443")).HandleConnect(goproxy.AlwaysMitm)

	for _, matchUrl := range Config.UrlMatches {
		proxy.OnRequest(goproxy.UrlMatches(regexp.MustCompile(matchUrl))).DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
			i := <-index
			add <- 0
			log.Info("使用第", i, "个", Config.CDN[i])
			// if r.URL.Scheme == "https" {
			// r.URL.Scheme = "http"
			// }
			// r.Header.Set("Connection", "close")
			r.Header.Add("If-Modified-Since", time.Now().Local().Format(time.RFC1123Z))
			r.Header.Add("If-None-Match", strconv.FormatInt(time.Now().UnixNano(), 10))
			r.Header.Add("Cache-Control", "no-cache")
			r.Header.Del("Content-Length")
			resp, err := DoForWardRequest(Config.CDN[i], r)
			if err != nil {
				log.Error(Config.CDN[i], " OnRequest error:", err)
				return r, nil
			}
			log.Info(Config.CDN[i], "success!")
			return r, resp
		})
	}

	go ChangeCDN()
	log.Info("监听端口:", Config.ListenAddr)
	http.ListenAndServe(Config.ListenAddr, proxy)
}

//切换CDN下标
func ChangeCDN() {
	i := 0
	index <- i
	for {
		select {
		case <-add:
			i += 1
			i = i % len(Config.CDN)
			index <- i
		}
	}
}
func newForwardClientConn(forwardAddress, scheme string) (*httputil.ClientConn, error) {
	// var clientConn *httputil.ClientConn
	if "http" == scheme {
		conn, err := net.Dial("tcp", forwardAddress+":80")
		if err != nil {
			log.Error("newForwardClientConn net.Dial error:", err)
			return nil, err
		}
		return httputil.NewClientConn(conn, nil), nil
	} else {
		conn, err := tls.Dial("tcp", forwardAddress+":443", &tls.Config{
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Error("newForwardClientConn tls.Dial error:", err)
			return nil, err
		}
		return httputil.NewClientConn(conn, nil), nil
	}
	//resp, err := clientConn.Do(req)
	return nil, nil
}

func DoForWardRequest(forwardAddress string, req *http.Request) (*http.Response, error) {
	clientConn, err := newForwardClientConn(forwardAddress, "https")
	if err != nil {
		log.Error("DoForWardRequest newForwardClientConn error:", err)
		return nil, err
	}
	// defer clientConn.Close()
	return clientConn.Do(req)
}

func DoForWardRequest2(forwardAddress string, req *http.Request) (*http.Response, error) {
	if !strings.Contains(forwardAddress, ":") {
		forwardAddress = forwardAddress + ":443"
	}

	conn, err := tls.Dial("tcp", forwardAddress, &tls.Config{
		InsecureSkipVerify: true,
	})
	// conn, err := net.Dial("tcp", forwardAddress)

	if err != nil {
		log.Error("doForWardRequest DialTimeout error:", err)
		return nil, err
	}
	// defer conn.Close()
	buf_forward_conn := bufio.NewReader(conn)

	errWrite := req.Write(conn)
	if errWrite != nil {
		log.Error("doForWardRequest Write error:", errWrite)
		return nil, err
	}

	return http.ReadResponse(buf_forward_conn, req)
}

//读取配置文件
func ReadConfig() error {
	if _, err := toml.DecodeFile("config.ini", &Config); err != nil {
		log.Error(err)
		return err
	}

	if Config.Timeout > 0 {
		timeout = time.Duration(Config.Timeout) * time.Second
	}
	return nil
}
