package rpc

import (
	"capnproto.org/go/capnp/v3/internal/syncutil"
)

var (
	with    = syncutil.With
	without = syncutil.Without
)
