package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// WritebackPolicy records how an action write became durable. It is included in
// the audit event emitted by ActionWriteTransaction.Commit.
type WritebackPolicy string

const (
	WritebackPolicyImmediate WritebackPolicy = "immediate"
	WritebackPolicyStaged    WritebackPolicy = "staged"
)

// ActionWriteStores is the minimal OSV2.10 store set needed to atomically commit
// action writebacks in tests and local runtimes. Cassandra deployments can wrap
// these interfaces with a logged/batched implementation without changing action
// handlers.
type ActionWriteStores struct {
	Objects  storage.ObjectStore
	Links    storage.LinkStore
	Actions  storage.ActionLogStore
	Branches storage.BranchOverlayStore
}

type stagedObjectWrite struct {
	object          storage.Object
	expectedVersion *uint64
}

type stagedLinkWrite struct {
	link   storage.Link
	delete bool
}

// ActionWriteTransaction stages object/link mutations and materializes them only
// when Commit is called. This gives Functions/action runtimes a concrete
// commit() boundary: staged writes are inert until Commit, and Commit emits one
// audit event carrying actor and writeback_policy metadata.
type ActionWriteTransaction struct {
	stores  ActionWriteStores
	tenant  storage.TenantId
	actor   string
	policy  WritebackPolicy
	action  string
	branch  storage.BranchID
	objects []stagedObjectWrite
	links   []stagedLinkWrite
}

// NewActionWriteTransaction creates an empty staged write transaction.
func NewActionWriteTransaction(stores ActionWriteStores, tenant storage.TenantId, actor string, policy WritebackPolicy, actionID string) *ActionWriteTransaction {
	if policy == "" {
		policy = WritebackPolicyImmediate
	}
	return &ActionWriteTransaction{stores: stores, tenant: tenant, actor: actor, policy: policy, action: actionID}
}

// NewBranchedActionWriteTransaction creates a transaction whose writes land in
// the branch overlay only. Merging the overlay into main is intentionally left to
// the Global Branching merge flow.
func NewBranchedActionWriteTransaction(stores ActionWriteStores, tenant storage.TenantId, branch storage.BranchID, actor string, policy WritebackPolicy, actionID string) *ActionWriteTransaction {
	tx := NewActionWriteTransaction(stores, tenant, actor, policy, actionID)
	tx.branch = branch
	return tx
}

// StageObjectPut stages a per-object row write. It does not touch storage until
// Commit is invoked.
func (tx *ActionWriteTransaction) StageObjectPut(object storage.Object, expectedVersion *uint64) {
	if tx == nil {
		return
	}
	if object.Tenant == "" {
		object.Tenant = tx.tenant
	}
	var expectedCopy *uint64
	if expectedVersion != nil {
		v := *expectedVersion
		expectedCopy = &v
	}
	tx.objects = append(tx.objects, stagedObjectWrite{object: object, expectedVersion: expectedCopy})
}

// StageLinkPut stages an affected link row write.
func (tx *ActionWriteTransaction) StageLinkPut(link storage.Link) {
	if tx == nil {
		return
	}
	if link.Tenant == "" {
		link.Tenant = tx.tenant
	}
	tx.links = append(tx.links, stagedLinkWrite{link: link})
}

// StageLinkDelete stages removal of an affected link row.
func (tx *ActionWriteTransaction) StageLinkDelete(link storage.Link) {
	if tx == nil {
		return
	}
	if link.Tenant == "" {
		link.Tenant = tx.tenant
	}
	tx.links = append(tx.links, stagedLinkWrite{link: link, delete: true})
}

func (tx *ActionWriteTransaction) auditEventSeed() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/action-write/%s/%s/%s/%s", tx.tenant, tx.branch, tx.action, tx.actor, tx.policy)
	for _, obj := range tx.objects {
		fmt.Fprintf(&b, "/object:%s@%d", obj.object.ID, obj.object.Version)
	}
	for _, link := range tx.links {
		op := "put"
		if link.delete {
			op = "delete"
		}
		fmt.Fprintf(&b, "/link:%s:%s:%s:%s", op, link.link.LinkType, link.link.From, link.link.To)
	}
	return b.String()
}

// Commit materializes all staged rows and appends the audit event. Store writes
// are applied only here; callers that never call Commit leave no partial object
// or link rows behind.
func (tx *ActionWriteTransaction) Commit(ctx context.Context) error {
	if tx == nil {
		return fmt.Errorf("nil action write transaction")
	}
	if tx.branch != "" {
		if tx.stores.Branches == nil {
			return fmt.Errorf("branched action write transaction missing branch overlay store")
		}
		for _, obj := range tx.objects {
			if _, err := tx.stores.Branches.PutBranchObject(ctx, tx.branch, obj.object, obj.expectedVersion); err != nil {
				return fmt.Errorf("commit branch action object write %s: %w", obj.object.ID, err)
			}
		}
		for _, link := range tx.links {
			if link.delete {
				if _, err := tx.stores.Branches.DeleteBranchLink(ctx, tx.branch, link.link.Tenant, link.link.LinkType, link.link.From, link.link.To); err != nil {
					return fmt.Errorf("commit branch action link delete %s/%s/%s: %w", link.link.LinkType, link.link.From, link.link.To, err)
				}
				continue
			}
			if err := tx.stores.Branches.PutBranchLink(ctx, tx.branch, link.link); err != nil {
				return fmt.Errorf("commit branch action link write %s/%s/%s: %w", link.link.LinkType, link.link.From, link.link.To, err)
			}
		}
	} else {
		if tx.stores.Objects == nil && len(tx.objects) > 0 {
			return fmt.Errorf("action write transaction missing object store")
		}
		if tx.stores.Links == nil && len(tx.links) > 0 {
			return fmt.Errorf("action write transaction missing link store")
		}
		for _, obj := range tx.objects {
			if _, err := tx.stores.Objects.Put(ctx, obj.object, obj.expectedVersion); err != nil {
				return fmt.Errorf("commit action object write %s: %w", obj.object.ID, err)
			}
		}
		for _, link := range tx.links {
			if link.delete {
				if _, err := tx.stores.Links.Delete(ctx, link.link.Tenant, link.link.LinkType, link.link.From, link.link.To); err != nil {
					return fmt.Errorf("commit action link delete %s/%s/%s: %w", link.link.LinkType, link.link.From, link.link.To, err)
				}
				continue
			}
			if err := tx.stores.Links.Put(ctx, link.link); err != nil {
				return fmt.Errorf("commit action link write %s/%s/%s: %w", link.link.LinkType, link.link.From, link.link.To, err)
			}
		}
	}
	if tx.stores.Actions != nil {
		payload, err := json.Marshal(map[string]any{
			"actor":            tx.actor,
			"writeback_policy": string(tx.policy),
			"object_writes":    len(tx.objects),
			"link_writes":      len(tx.links),
			"branch_id":        string(tx.branch),
		})
		if err != nil {
			return err
		}
		eventID := uuid.NewSHA1(OntologyNamespace, []byte(tx.auditEventSeed())).String()
		if err := tx.stores.Actions.Append(ctx, storage.ActionLogEntry{Tenant: tx.tenant, EventID: &eventID, ActionID: tx.action, Kind: "action.writeback_committed", Subject: tx.actor, Payload: payload, RecordedAtMs: time.Now().UTC().UnixMilli()}); err != nil {
			return fmt.Errorf("append action write audit event: %w", err)
		}
	}
	return nil
}
