// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package rabbit

type Router map[string]func(message Message)

func NewRouter() Router {
	return make(Router)
}

func (r Router) Add(key string, f func(message Message)) {
	r[key] = f
}
