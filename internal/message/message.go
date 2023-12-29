package message

import "github.com/blanu/radiowave"

type ImpactMessage struct {
	Payload []byte
}

func (m ImpactMessage) ToBytes() []byte {
	return m.Payload
}

type ImpactMessageFactory struct {
}

func NewImpactMessageFactory() ImpactMessageFactory {
	return ImpactMessageFactory{}
}

func (f ImpactMessageFactory) FromBytes(data []byte) (radiowave.Message, error) {
	return ImpactMessage{data}, nil
}
