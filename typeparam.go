package capnp

// The TypeParam interface must be satisified by a type to be used as a canpproto
// type parameter. This is satisified by all *T such that T is a capnproto pointer
// type (Note: The receiver must be a pointer so DecodeFromPtr can update it).
type TypeParam interface {
	// Convert the receiver to a Ptr, storing it in seg if it is not
	// already associated with some message (only true for Clients and
	// wrappers around them.
	EncodeAsPtr(seg *Segment) Ptr

	// Decode the pointer and store it in the receiver.
	DecodeFromPtr(p Ptr)
}
