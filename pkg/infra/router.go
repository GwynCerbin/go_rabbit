package infra

import "github.com/GwynCerbin/go_rabbit/pkg/broker"

type Router map[string]func(message broker.Message)

func NewRouter() Router {
	return make(Router)
}

func (r Router) Add(key string, f func(message broker.Message)) {
	r[key] = f
}
