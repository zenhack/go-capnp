package rpc_test

import (
	"context"
	"testing"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/pogs"
	"capnproto.org/go/capnp/v3/rpc"
	"capnproto.org/go/capnp/v3/rpc/internal/testcapnp"
	"capnproto.org/go/capnp/v3/rpc/internal/testnetwork"
	rpccp "capnproto.org/go/capnp/v3/std/capnp/rpc"
	"github.com/stretchr/testify/require"
	"zenhack.net/go/util/deferred"
)

type rpcProvide struct {
	QuestionID uint32 `capnp:"questionId"`
	Target     rpcMessageTarget
	Recipient  capnp.Ptr
}

func TestSendProvide(t *testing.T) {
	// Note: we do our deferring in this test via a deferred.Queue,
	// so we can be sure that cancelling the context happens *first.*
	// Otherwise, some of the things we defer can block until
	// connection shutdown which won't happen until the context ends,
	// causing this test to deadlock instead of failing with a useful
	// error.
	dq := deferred.Queue{}
	defer dq.Run()
	ctx, cancel := context.WithCancel(context.Background())
	dq.Defer(cancel)

	j := testnetwork.NewJoiner()

	pp := testcapnp.PingPong_ServerToClient(&pingPonger{})
	dq.Defer(pp.Release)

	cfgOpts := func(opts *rpc.Options) {
		opts.ErrorReporter = testErrorReporter{tb: t}
	}

	introducer := j.Join(cfgOpts)
	recipient := j.Join(cfgOpts)
	provider := j.Join(cfgOpts)

	go introducer.Serve(ctx)

	rConn, err := introducer.Dial(recipient.LocalID())
	require.NoError(t, err)

	pConn, err := introducer.Dial(provider.LocalID())
	require.NoError(t, err)

	rBs := rConn.Bootstrap(ctx)
	dq.Defer(rBs.Release)
	pBs := pConn.Bootstrap(ctx)
	dq.Defer(pBs.Release)

	rTrans, err := recipient.DialTransport(introducer.LocalID())
	require.NoError(t, err)

	pTrans, err := provider.DialTransport(introducer.LocalID())
	require.NoError(t, err)

	bootstrapExportID := uint32(10)
	doBootstrap := func(trans rpc.Transport) {
		// Receive bootstrap
		rmsg, release, err := recvMessage(ctx, trans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_bootstrap, rmsg.Which)
		qid := rmsg.Bootstrap.QuestionID

		// Write back return
		outMsg, err := trans.NewMessage()
		require.NoError(t, err, "trans.NewMessage()")
		iptr := capnp.NewInterface(outMsg.Message().Segment(), 0)
		require.NoError(t, pogs.Insert(rpccp.Message_TypeID, capnp.Struct(outMsg.Message()), &rpcMessage{
			Which: rpccp.Message_Which_return,
			Return: &rpcReturn{
				AnswerID: qid,
				Which:    rpccp.Return_Which_results,
				Results: &rpcPayload{
					Content: iptr.ToPtr(),
					CapTable: []rpcCapDescriptor{
						{
							Which:        rpccp.CapDescriptor_Which_senderHosted,
							SenderHosted: bootstrapExportID,
						},
					},
				},
			},
		}))
		require.NoError(t, outMsg.Send())

		// Receive finish
		rmsg, release, err = recvMessage(ctx, trans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_finish, rmsg.Which)
		require.Equal(t, qid, rmsg.Finish.QuestionID)
	}
	doBootstrap(rTrans)
	require.NoError(t, rBs.Resolve(ctx))
	doBootstrap(pTrans)
	require.NoError(t, pBs.Resolve(ctx))

	futEmpty, rel := testcapnp.EmptyProvider(pBs).GetEmpty(ctx, nil)
	dq.Defer(rel)

	emptyExportID := uint32(30)
	{
		// Receive call
		rmsg, release, err := recvMessage(ctx, pTrans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_call, rmsg.Which)
		qid := rmsg.Call.QuestionID
		require.Equal(t, uint64(testcapnp.EmptyProvider_TypeID), rmsg.Call.InterfaceID)
		require.Equal(t, uint16(0), rmsg.Call.MethodID)

		// Send return
		outMsg, err := pTrans.NewMessage()
		require.NoError(t, err)
		seg := outMsg.Message().Segment()
		results, err := capnp.NewStruct(seg, capnp.ObjectSize{
			PointerCount: 1,
		})
		require.NoError(t, err)
		iptr := capnp.NewInterface(seg, 0)
		results.SetPtr(0, iptr.ToPtr())
		require.NoError(t, sendMessage(ctx, pTrans, &rpcMessage{
			Which: rpccp.Message_Which_return,
			Return: &rpcReturn{
				Which: rpccp.Return_Which_results,
				Results: &rpcPayload{
					Content: results.ToPtr(),
					CapTable: []rpcCapDescriptor{
						{
							Which:        rpccp.CapDescriptor_Which_senderHosted,
							SenderHosted: emptyExportID,
						},
					},
				},
			},
		}))

		// Receive finish
		rmsg, release, err = recvMessage(ctx, pTrans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_finish, rmsg.Which)
		require.Equal(t, qid, rmsg.Finish.QuestionID)
	}

	resEmpty, err := futEmpty.Struct()
	require.NoError(t, err)
	empty := resEmpty.Empty()

	_, rel = testcapnp.CapArgsTest(rBs).Call(ctx, func(p testcapnp.CapArgsTest_call_Params) error {
		return p.SetCap(capnp.Client(empty))
	})
	dq.Defer(rel)

	//var provideQid uint32
	{
		// Provider should receive a provide message
		rmsg, release, err := recvMessage(ctx, pTrans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_provide, rmsg.Which)
		//provideQid = rmsg.Provide.QuestionID
		require.Equal(t, rpccp.MessageTarget_Which_importedCap, rmsg.Provide.Target.Which)
		require.Equal(t, emptyExportID, rmsg.Provide.Target.ImportedCap)
	}

	{
		// Read the call; should start off with a promise, record the ID:
		rmsg, release, err := recvMessage(ctx, rTrans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_call, rmsg.Which)
		call := rmsg.Call
		//qid := call.QuestionID
		require.Equal(t, rpcMessageTarget{
			Which:       rpccp.MessageTarget_Which_importedCap,
			ImportedCap: bootstrapExportID,
		}, call.Target)

		require.Equal(t, uint64(testcapnp.CapArgsTest_TypeID), call.InterfaceID)
		require.Equal(t, uint16(0), call.MethodID)
		ptr, err := call.Params.Content.Struct().Ptr(0)
		require.NoError(t, err)
		iptr := ptr.Interface()
		require.True(t, iptr.IsValid())
		require.Equal(t, capnp.CapabilityID(0), iptr.Capability())
		require.Equal(t, 1, len(call.Params.CapTable))
		desc := call.Params.CapTable[0]
		require.Equal(t, rpccp.CapDescriptor_Which_senderPromise, desc.Which)
		promiseExportID := desc.SenderPromise

		// Read the resolve for that promise, which should point to a third party cap:
		rmsg, release, err = recvMessage(ctx, rTrans)
		require.NoError(t, err)
		dq.Defer(release)
		require.Equal(t, rpccp.Message_Which_resolve, rmsg.Which)
		require.Equal(t, promiseExportID, rmsg.Resolve.PromiseID)
		require.Equal(t, rpccp.Resolve_Which_cap, rmsg.Resolve.Which)
		capDesc := rmsg.Resolve.Cap
		require.Equal(t, rpccp.CapDescriptor_Which_thirdPartyHosted, capDesc.Which)
	}
	panic("TODO: finish this up")
}
