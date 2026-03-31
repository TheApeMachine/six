//go:build unix

// Copyright (c) 2024 Robert Clausecker <fuz@fuz.su>

package pospop

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func mapGuarded() (mapping []byte, slice []byte, err error) {
	pagesize := unix.Getpagesize()
	mapping, err = unix.Mmap(-1, 0, 3*pagesize, unix.PROT_NONE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil, nil, err
	}

	slice = mapping[pagesize : 2*pagesize : 2*pagesize]
	err = unix.Mprotect(slice, unix.PROT_READ|unix.PROT_WRITE)
	if err != nil {
		unix.Munmap(mapping)
		return nil, nil, err
	}

	return
}

func TestOverread(t *testing.T) {
	for index := range count64funcs {
		t.Run(count64funcs[index].name, func(tt *testing.T) {
			if !count64funcs[index].available {
				tt.SkipNow()
			}

			testOverread(tt, count64funcs[index].count64)
		})
	}
}

func testOverread(t *testing.T, count64 func(*[64]int, []uint64)) {
	var counters [64]int

	mapping, slice, err := mapGuarded()
	defer unix.Munmap(mapping)
	if err != nil {
		t.Log("Cannot allocate memory:", err)
		t.SkipNow()
	}

	words := unsafe.Slice((*uint64)(unsafe.Pointer(&slice[0])), len(slice)/8)

	for start := 0; start < 64 && start < len(words); start++ {
		for end := len(words) - 64; end <= len(words); end++ {
			if end >= start {
				count64(&counters, words[start:end])
			}
		}
	}

	for start := 0; start < 64; start++ {
		for end := start; end <= 64 && end <= len(words); end++ {
			count64(&counters, words[start:end])
		}
	}

	for start := len(words) - 64; start <= len(words); start++ {
		for end := start; end <= len(words); end++ {
			count64(&counters, words[start:end])
		}
	}
}
