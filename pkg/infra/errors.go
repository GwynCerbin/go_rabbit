// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package infra

type EmptyRoutError struct {
}

func (EmptyRoutError) Error() string {
	return "empty route"
}

type UnroutedMessage struct {
}

func (UnroutedMessage) Error() string {
	return "unrouted message"
}

type ConsumerCloseError struct {
}

func (ConsumerCloseError) Error() string {
	return "close consumer, dropped with error"
}
