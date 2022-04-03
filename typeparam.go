package capnp

// The TypeParam interface must be satisified by a type to be used as a canpproto
// type parameter. This is satisified by all capnproto pointer types.
type TypeParam interface {
	// Convert the receiver to a Ptr, storing it in seg if it is not
	// already associated with some message (only true for Clients and
	// wrappers around them.
	EncodeAsPtr(seg *Segment) Ptr

	// Decode the pointer into the receiver. Generally, the reciever will
	// be a pointer to the zero value prior to the call.
	DecodeFromPtr(p Ptr)
}
