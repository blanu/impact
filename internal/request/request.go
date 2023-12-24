package request

import "internal/response"

type Request struct {
	Payload      []byte
	ReplyChannel chan response.Response
}
