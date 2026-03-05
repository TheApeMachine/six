package logic

import (
"fmt"
"testing"
"unsafe"
"github.com/theapemachine/six/geometry"
)

func TestStructSize(t *testing.T) {
	fmt.Printf("\n=== STRUCT SIZES ===\n")
	fmt.Printf("Manifold Size: %d\n", unsafe.Sizeof(geometry.IcosahedralManifold{}))
	
	arr := make([]geometry.IcosahedralManifold, 3)
	fmt.Printf("Offset of arr[0]: %p\n", &arr[0])
	fmt.Printf("Offset of arr[1]: %p\n", &arr[1])
	fmt.Printf("Offset of arr[2]: %p\n", &arr[2])
	
	bytesBetween := uintptr(unsafe.Pointer(&arr[1])) - uintptr(unsafe.Pointer(&arr[0]))
	fmt.Printf("Bytes between elements in array: %d\n", bytesBetween)
	fmt.Printf("====================\n")
}
