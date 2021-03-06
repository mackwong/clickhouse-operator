package controller

import (
	"github.com/mackwong/clickhouse-operator/pkg/controller/clickhousecluster"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, clickhousecluster.Add)
}
