package domain

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kernelstores "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestActionWriteTransactionMaterializesOnlyOnCommitAndAudits(t *testing.T) {
	objects := kernelstores.NewInMemoryObjectStore()
	links := kernelstores.NewInMemoryLinkStore()
	actions := kernelstores.NewInMemoryActionLogStore()
	tx := NewActionWriteTransaction(ActionWriteStores{Objects: objects, Links: links, Actions: actions}, "acme", "actor-1", WritebackPolicyStaged, "action-1")
	tx.StageObjectPut(storage.Object{ID: "obj-1", TypeID: "Aircraft", Version: 1, Payload: json.RawMessage(`{"tail":"EC-1"}`)}, nil)
	tx.StageLinkPut(storage.Link{LinkType: "owns.asset", From: "obj-1", To: "asset-1", Payload: json.RawMessage(`{}`)})

	got, err := objects.Get(context.Background(), "acme", "obj-1", storage.Strong())
	require.NoError(t, err)
	assert.Nil(t, got)
	beforeLinks, err := links.ListOutgoing(context.Background(), "acme", "owns.asset", "obj-1", storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	assert.Empty(t, beforeLinks.Items)

	require.NoError(t, tx.Commit(context.Background()))
	got, err = objects.Get(context.Background(), "acme", "obj-1", storage.Strong())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.TypeId("Aircraft"), got.TypeID)
	afterLinks, err := links.ListOutgoing(context.Background(), "acme", "owns.asset", "obj-1", storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	require.Len(t, afterLinks.Items, 1)

	audit, err := actions.ListForAction(context.Background(), "acme", "action-1", storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	require.Len(t, audit.Items, 1)
	assert.Equal(t, "action.writeback_committed", audit.Items[0].Kind)
	assert.JSONEq(t, `{"actor":"actor-1","writeback_policy":"staged","object_writes":1,"link_writes":1,"branch_id":""}`, string(audit.Items[0].Payload))
}

func TestBranchedActionWriteTransactionWritesOverlayOnly(t *testing.T) {
	objects := kernelstores.NewInMemoryObjectStore()
	links := kernelstores.NewInMemoryLinkStore()
	actions := kernelstores.NewInMemoryActionLogStore()
	branches := storage.NewInMemoryOSV2AdvancedStore()
	tx := NewBranchedActionWriteTransaction(ActionWriteStores{Objects: objects, Links: links, Actions: actions, Branches: branches}, "acme", "branch-1", "actor-1", WritebackPolicyStaged, "action-branch")
	tx.StageObjectPut(storage.Object{ID: "obj-branch", TypeID: "Aircraft", Version: 1, Payload: json.RawMessage(`{"tail":"BR"}`)}, nil)
	tx.StageLinkPut(storage.Link{LinkType: "owns.asset", From: "obj-branch", To: "asset-branch", Payload: json.RawMessage(`{}`)})

	require.NoError(t, tx.Commit(context.Background()))
	mainObj, err := objects.Get(context.Background(), "acme", "obj-branch", storage.Strong())
	require.NoError(t, err)
	assert.Nil(t, mainObj, "branch writes must not hit main object store")
	branchObj, err := branches.GetBranchObject(context.Background(), "branch-1", "acme", "obj-branch", objects, storage.Strong())
	require.NoError(t, err)
	require.NotNil(t, branchObj)
	assert.Equal(t, storage.TypeId("Aircraft"), branchObj.TypeID)

	mainLinks, err := links.ListOutgoing(context.Background(), "acme", "owns.asset", "obj-branch", storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	assert.Empty(t, mainLinks.Items, "branch writes must not hit main link store")

	audit, err := actions.ListForAction(context.Background(), "acme", "action-branch", storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	require.Len(t, audit.Items, 1)
	assert.JSONEq(t, `{"actor":"actor-1","writeback_policy":"staged","object_writes":1,"link_writes":1,"branch_id":"branch-1"}`, string(audit.Items[0].Payload))
}
