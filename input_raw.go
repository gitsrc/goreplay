package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/buger/goreplay/proto"
	raw "github.com/buger/goreplay/raw_socket_listener"
)

// RAWInput used for intercepting traffic for given address
type RAWInput struct {
	data          chan *raw.TCPMessage
	address       string
	expire        time.Duration
	quit          chan bool
	engine        int
	realIPHeader  []byte
	trackResponse bool
	listener      *raw.Listener
	bpfFilter     string
	timestampType string
	bufferSize    int
}

// Available engines for intercepting traffic
const (
	EngineRawSocket = 1 << iota
	EnginePcap
	EnginePcapFile
)

// NewRAWInput constructor for RAWInput. Accepts address with port as argument.
func NewRAWInput(address string, engine int, trackResponse bool, expire time.Duration, realIPHeader string, bpfFilter string, timestampType string, bufferSize int) (i *RAWInput) {
	i = new(RAWInput)
	i.data = make(chan *raw.TCPMessage)
	i.address = address
	i.expire = expire
	i.engine = engine
	i.bpfFilter = bpfFilter
	i.realIPHeader = []byte(realIPHeader)
	i.quit = make(chan bool)
	i.trackResponse = trackResponse
	i.timestampType = timestampType
	i.bufferSize = bufferSize

	i.listen(address)
	i.listener.IsReady()

	return
}

func (i *RAWInput) Read(data []byte) (int, error) {
	msg := <-i.data
	buf := msg.Bytes()

	var header []byte

	if msg.IsIncoming {
		header = payloadHeader(RequestPayload, msg.UUID(), msg.Start.UnixNano(), -1)

		if len(i.realIPHeader) > 0 {
			buf = proto.SetHeader(buf, i.realIPHeader, []byte(msg.IP().String()))
		}
	} else {
		header = payloadHeader(ResponsePayload, msg.UUID(), msg.Start.UnixNano(), msg.End.UnixNano()-msg.AssocMessage.End.UnixNano())
	}

	//针对header进行处理
	//buf = proto.DeleteHeader(buf, []byte("User-Agent"))

	//body := proto.Body(buf)

	//如果是reponse
	if !isRequestPayload(buf) {
		isChunk := false
		chunkBytes := bytes.ToUpper(proto.Header(buf, []byte("Transfer-Encoding")))

		if bytes.Equal(chunkBytes, bytes.ToUpper([]byte("chunked"))) {
			isChunk = true
		}

		//如果是reponse响应
		if !isRequestPayload(buf) {

			responseBody := proto.Body(buf)

			bodyStartPos := proto.MIMEHeadersEndPos(buf)
			//bodyBytes := make([]byte, 0)
			//如果response信息体不为空
			if len(responseBody) > 0 {
				if isChunk {
					//chunk模式
					lengthEndPos := bytes.Index(buf[bodyStartPos:], proto.CLRF)
					lengthEndPos = bodyStartPos + lengthEndPos
					lenHex := string(buf[bodyStartPos:lengthEndPos])

					chunkSize, err := strconv.ParseInt(lenHex, 16, 32)
					if err == nil {
						log.Println(chunkSize)
						jm := make(map[string]interface{})

						err := json.Unmarshal(buf[lengthEndPos:int64(lengthEndPos)+chunkSize+2], &jm)

						if err == nil {
							log.Println("origin json string:", string(buf[lengthEndPos:int64(lengthEndPos)+chunkSize+2]))
							log.Println("origin json map:", jm)

							ProcessingMap(jm)

							log.Println("nowing json map:", jm)
							newData, err := json.Marshal(jm)
							if err == nil {
								log.Println("nowing json string:", string(newData))
							}

						}

					}

				} else {
					//非chunk模式
				}
			}

			// log.Println("is chunk")
			// log.Println("reponse-666", proto.Body(buf))
			// log.Println("reponse-777", string(proto.Body(buf)))
		}

	}

	copy(data[0:len(header)], header)

	copy(data[len(header):], buf)

	return len(buf) + len(header), nil
}

func ProcessingMap(data map[string]interface{}) {
	for k, v := range data {
		switch v.(type) {
		case string:
			data[k] = "xxx" //消除map 里面的字符串
		case map[string]interface{}:
			ProcessingMap(data[k].(map[string]interface{}))
		}
	}
}

func (i *RAWInput) listen(address string) {
	Debug("Listening for traffic on: " + address)

	host, port, err := net.SplitHostPort(address) 

	if err != nil {
		log.Fatal("input-raw: error while parsing address", err)
	}

	i.listener = raw.NewListener(host, port, i.engine, i.trackResponse, i.expire, i.bpfFilter, i.timestampType, i.bufferSize, Settings.inputRAWOverrideSnapLen, Settings.inputRAWImmediateMode)

	ch := i.listener.Receiver()

	go func() {
		for {
			select {
			case <-i.quit:
				return
			default:
			}

			// Receiving TCPMessage object
			m := <-ch

			i.data <- m
		}
	}()
}

func (i *RAWInput) String() string {
	return "Intercepting traffic from: " + i.address
}

func (i *RAWInput) Close() error {
	i.listener.Close()
	close(i.quit)
	return nil
}
