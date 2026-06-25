// Copyright 2018 ORAS authors
// Copyright 2024 Defense Unicorns authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from pkg:
// https://github.com/defenseunicorns/pkg
//
// Modifications Copyright 2026 colonel-byte.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cache are a sub-section of function from defense-unicorn pkg cache package
package cache

import (
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
)

// Cache target struct.
type target struct {
	oras.ReadOnlyTarget
	cache content.Storage
}

// New generates a new target storage with caching.
func New(source oras.ReadOnlyTarget, cache content.Storage) oras.ReadOnlyTarget {
	t := &target{
		ReadOnlyTarget: source,
		cache:          cache,
	}
	if refFetcher, ok := source.(registry.ReferenceFetcher); ok {
		return &referenceTarget{
			target:           t,
			ReferenceFetcher: refFetcher,
		}
	}
	return t
}

// Cache referenceTarget struct.
type referenceTarget struct {
	*target
	registry.ReferenceFetcher
}
