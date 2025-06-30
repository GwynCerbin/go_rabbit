// SPDX-License-Identifier: MIT
// Copyright © 2024–2026 Alexander Demin

package infra

type EmptyRoutError struct {
}

func (EmptyRoutError) Error() string {
	return "empty route"
}
