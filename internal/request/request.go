package request

import (
	"github.com/blanu/radiowave"
)

type Request struct {
	Message      radiowave.Message
	ReplyChannel chan radiowave.Message
}
