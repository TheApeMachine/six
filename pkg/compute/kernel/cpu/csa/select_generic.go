// Copyright (c) 2020, 2024 Robert Clausecker <fuz@fuz.su>

//go:build !amd64 && !arm64

package pospop

var count64funcs = []count64impl{{count64generic, "generic", true}}
