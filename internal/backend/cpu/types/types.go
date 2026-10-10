package types

import "github.com/aayushkdev/perfmon/internal/model"

type Info struct {
	Vendor string
	Model  string
}

type Classifier interface {
	Name() string
	Classify(core model.CPUCore, info Info) model.CoreType
}
