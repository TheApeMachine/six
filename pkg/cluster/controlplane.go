package cluster

import "github.com/theapemachine/six/pkg/store"

type ControlPlane struct {
	lsm store.SpatialIndex
}

func NewControlPlane() *ControlPlane {
	return &ControlPlane{}
}
