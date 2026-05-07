package jwksrotation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Pure logic ---------------------------------------------------------

func TestStableKidFormat(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openfoundry-jwt-v1", StableKid(VaultKeyRef{Name: "openfoundry-jwt", Version: 1}))
	assert.Equal(t, "openfoundry-jwt-v42", StableKid(VaultKeyRef{Name: "openfoundry-jwt", Version: 42}))
}

func TestParseJwksKeyStatusKnownValues(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"active", "grace", "retired"} {
		got, err := ParseJwksKeyStatus(v)
		require.NoError(t, err)
		assert.Equal(t, JwksKeyStatus(v), got)
	}
}

func TestParseJwksKeyStatusUnknownIsStoreError(t *testing.T) {
	t.Parallel()
	_, err := ParseJwksKeyStatus("rotated")
	require.Error(t, err)
	assert.True(t, IsJwksStore(err))
	assert.Contains(t, err.Error(), "unknown JWKS key status rotated")
}

func TestRotationPolicyDefaults(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(90), ASVSL2Default.ActiveDays)
	assert.Equal(t, int64(14), ASVSL2Default.GraceDays)
}

func TestRotationPolicyRotateAndRetire(t *testing.T) {
	t.Parallel()
	activated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rotateAt, retireAt := ASVSL2Default.RotateAndRetire(activated)
	assert.Equal(t, activated.AddDate(0, 0, 90), rotateAt)
	assert.Equal(t, activated.AddDate(0, 0, 104), retireAt)
}

func TestRotationPolicyIsInGrace(t *testing.T) {
	t.Parallel()
	prevActivated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Day 89 of cycle: still in active-only window.
	assert.False(t, ASVSL2Default.IsInGrace(prevActivated, prevActivated.AddDate(0, 0, 89)))
	// Day 90 of cycle: rotation moment, prev key in grace.
	assert.True(t, ASVSL2Default.IsInGrace(prevActivated, prevActivated.AddDate(0, 0, 90)))
	// Day 103: still in grace.
	assert.True(t, ASVSL2Default.IsInGrace(prevActivated, prevActivated.AddDate(0, 0, 103)))
	// Day 104: just retired.
	assert.False(t, ASVSL2Default.IsInGrace(prevActivated, prevActivated.AddDate(0, 0, 104)))
}

// --- Errors --------------------------------------------------------------

func TestErrorClassificationHelpers(t *testing.T) {
	t.Parallel()
	store := &JwksRotationError{Kind: ErrJwksStore, Message: "x"}
	state := &JwksRotationError{Kind: ErrJwksState, Message: "x"}
	vault := &JwksRotationError{Kind: ErrJwksVault, Message: "x"}

	assert.True(t, IsJwksStore(store))
	assert.False(t, IsJwksState(store))
	assert.True(t, IsJwksState(state))
	assert.False(t, IsJwksVault(state))
	assert.True(t, IsJwksVault(vault))
	assert.False(t, IsJwksStore(vault))
}

func TestErrorClassificationUnwraps(t *testing.T) {
	t.Parallel()
	wrapped := errors.Join(errors.New("transport"), &JwksRotationError{Kind: ErrJwksVault, Message: "y"})
	assert.True(t, IsJwksVault(wrapped))
}

func TestErrorMessages(t *testing.T) {
	t.Parallel()
	cases := map[JwksRotationErrorKind]string{
		ErrJwksStore: "jwks store error: m",
		ErrJwksState: "jwks key state error: m",
		ErrJwksVault: "vault transit error: m",
	}
	for kind, want := range cases {
		err := &JwksRotationError{Kind: kind, Message: "m"}
		assert.Equal(t, want, err.Error())
	}
}

func TestSignErrorWrapsTransport(t *testing.T) {
	t.Parallel()
	transport := errors.New("connection reset")
	err := WrapSignError("vault unreachable", transport)
	assert.Contains(t, err.Error(), "vault unreachable")
	assert.ErrorIs(t, err, transport)
}

// --- Pure builders ------------------------------------------------------

func TestBuildJwksFromRecordsActiveOnly(t *testing.T) {
	t.Parallel()
	active := JwksKeyRecord{Kid: "k-v1", Kty: "RSA", PublicPEM: "P1"}
	jwks := BuildJwksFromRecords(active, nil)
	require.Len(t, jwks.Keys, 1)
	assert.Equal(t, "active", jwks.Keys[0].Status)
	assert.Equal(t, "sig", jwks.Keys[0].Use)
	assert.Equal(t, "k-v1", jwks.Keys[0].Kid)
}

func TestBuildJwksFromRecordsActivePlusGrace(t *testing.T) {
	t.Parallel()
	active := JwksKeyRecord{Kid: "k-v2", Kty: "RSA", PublicPEM: "P2"}
	grace := []JwksKeyRecord{
		{Kid: "k-v1", Kty: "RSA", PublicPEM: "P1"},
	}
	jwks := BuildJwksFromRecords(active, grace)
	require.Len(t, jwks.Keys, 2)
	assert.Equal(t, "active", jwks.Keys[0].Status)
	assert.Equal(t, "k-v2", jwks.Keys[0].Kid)
	assert.Equal(t, "grace", jwks.Keys[1].Status)
	assert.Equal(t, "k-v1", jwks.Keys[1].Kid)
}

func TestBuildJwksWithPublicEntriesHonoursPolicy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	prev := PublicKeyEntry{
		Kid: "k-v1", Kty: "RSA", PublicPEM: "P1",
		ActivatedAt: now.AddDate(0, 0, -91), // in grace (rotated yesterday)
	}
	active := PublicKeyEntry{Kid: "k-v2", Kty: "RSA", PublicPEM: "P2", ActivatedAt: now}

	// In grace: both keys published.
	jwks := BuildJwks(active, &prev, ASVSL2Default, now)
	require.Len(t, jwks.Keys, 2)
	assert.Equal(t, "k-v2", jwks.Keys[0].Kid)
	assert.Equal(t, "active", jwks.Keys[0].Status)
	assert.Equal(t, "k-v1", jwks.Keys[1].Kid)
	assert.Equal(t, "grace", jwks.Keys[1].Status)

	// Out of grace: only the active key.
	prevOld := prev
	prevOld.ActivatedAt = now.AddDate(0, 0, -200)
	jwksOld := BuildJwks(active, &prevOld, ASVSL2Default, now)
	require.Len(t, jwksOld.Keys, 1)
	assert.Equal(t, "k-v2", jwksOld.Keys[0].Kid)
}

// --- InMemoryJwksKeyStore ------------------------------------------------

func TestInMemoryStoreActiveKeyEmpty(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	got, err := s.ActiveKey(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestInMemoryStoreUpsertActiveSeed(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	rec := JwksKeyRecord{
		Kid: "k-v1", Kty: "RSA", PublicPEM: "P1",
		VaultKeyName: "k", VaultKeyVersion: 1,
		Status:      StatusActive,
		ActivatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, s.UpsertActiveSeed(context.Background(), rec))
	got, err := s.ActiveKey(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "k-v1", got.Kid)
	assert.Equal(t, StatusActive, got.Status)
}

func TestInMemoryStoreRotateToDemotesPrevious(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStore()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	prev := JwksKeyRecord{Kid: "k-v1", Status: StatusActive, ActivatedAt: now.AddDate(0, 0, -91)}
	next := JwksKeyRecord{Kid: "k-v2", Status: StatusActive, ActivatedAt: now}
	graceUntil := now.AddDate(0, 0, 14)

	require.NoError(t, s.UpsertActiveSeed(ctx, prev))
	require.NoError(t, s.RotateTo(ctx, prev, next, graceUntil))

	active, err := s.ActiveKey(ctx)
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, "k-v2", active.Kid)

	grace, err := s.GraceKeys(ctx, now)
	require.NoError(t, err)
	require.Len(t, grace, 1)
	assert.Equal(t, "k-v1", grace[0].Kid)
	require.NotNil(t, grace[0].RetireAfter)
	assert.True(t, grace[0].RetireAfter.Equal(graceUntil))
}

func TestInMemoryStoreGraceKeysFiltersExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStore()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	prev := JwksKeyRecord{Kid: "k-v1", Status: StatusActive, ActivatedAt: now.AddDate(0, 0, -91)}
	next := JwksKeyRecord{Kid: "k-v2", Status: StatusActive, ActivatedAt: now}
	require.NoError(t, s.UpsertActiveSeed(ctx, prev))
	require.NoError(t, s.RotateTo(ctx, prev, next, now.AddDate(0, 0, 14)))

	// Querying after retire_after returns no grace rows.
	grace, err := s.GraceKeys(ctx, now.AddDate(0, 0, 30))
	require.NoError(t, err)
	assert.Empty(t, grace)
}

func TestInMemoryStoreRollbackTo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStore()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	v1 := JwksKeyRecord{Kid: "k-v1", Status: StatusActive, ActivatedAt: now.AddDate(0, 0, -91)}
	v2 := JwksKeyRecord{Kid: "k-v2", Status: StatusActive, ActivatedAt: now}
	require.NoError(t, s.UpsertActiveSeed(ctx, v1))
	require.NoError(t, s.RotateTo(ctx, v1, v2, now.AddDate(0, 0, 14)))

	// Now rollback to v1.
	require.NoError(t, s.RollbackTo(ctx, v1, v2, now.AddDate(0, 0, 14)))
	active, err := s.ActiveKey(ctx)
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, "k-v1", active.Kid)
	grace, err := s.GraceKeys(ctx, now)
	require.NoError(t, err)
	require.Len(t, grace, 1)
	assert.Equal(t, "k-v2", grace[0].Kid)
}

// --- FakeTransitKeyClient ----------------------------------------------

func TestFakeTransitKeyClientRotateBumpsVersion(t *testing.T) {
	t.Parallel()
	c := NewFakeTransitKeyClient("openfoundry-jwt")
	got, err := c.RotateKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint32(2), got.Version)
	got2, err := c.RotateKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint32(3), got2.Version)
	latest, err := c.LatestKeyRef(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint32(3), latest.Version)
}

func TestFakeTransitKeyClientPublicKeyPEMIsStableKid(t *testing.T) {
	t.Parallel()
	c := NewFakeTransitKeyClient("k")
	pem, err := c.PublicKeyPEM(context.Background(), VaultKeyRef{Name: "k", Version: 7})
	require.NoError(t, err)
	assert.Equal(t, "PEM(k-v7)", pem)
}

// --- Service orchestrator ------------------------------------------------

func TestServicePublishedJwksSeedsOnFirstCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	jwks, err := s.PublishedJwks(ctx, now)
	require.NoError(t, err)
	require.Len(t, jwks.Keys, 1, "first publish seeds the store and returns 1 active")
	assert.Equal(t, "k-v1", jwks.Keys[0].Kid)
	assert.Equal(t, "active", jwks.Keys[0].Status)
}

func TestServiceRotateThenPublishYieldsTwoKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewInMemoryStore()
	transit := NewFakeTransitKeyClient("k")
	s := NewService(store, transit, ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	out, err := s.Rotate(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, "k-v1", out.PreviousActiveKid)
	assert.Equal(t, "k-v2", out.ActiveKid)
	assert.True(t, out.GraceUntil.Equal(now.AddDate(0, 0, 14)))

	jwks, err := s.PublishedJwks(ctx, now)
	require.NoError(t, err)
	require.Len(t, jwks.Keys, 2)
	assert.Equal(t, "k-v2", jwks.Keys[0].Kid)
	assert.Equal(t, "active", jwks.Keys[0].Status)
	assert.Equal(t, "k-v1", jwks.Keys[1].Kid)
	assert.Equal(t, "grace", jwks.Keys[1].Status)
}

func TestServiceRollbackRestoresPreviousActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Rotate(ctx, now)
	require.NoError(t, err)

	out, err := s.Rollback(ctx, nil, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "k-v1", out.RestoredActiveKid)
	assert.Equal(t, "k-v2", out.DemotedKid)

	jwks, err := s.PublishedJwks(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, jwks.Keys, 2)
	assert.Equal(t, "k-v1", jwks.Keys[0].Kid)
	assert.Equal(t, "active", jwks.Keys[0].Status)
}

func TestServiceRollbackTargetKidNotInGrace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Rotate(ctx, now)
	require.NoError(t, err)

	bogus := "does-not-exist"
	_, err = s.Rollback(ctx, &bogus, now)
	require.Error(t, err)
	assert.True(t, IsJwksState(err))
	assert.Contains(t, err.Error(), bogus)
}

func TestServiceRollbackEmptyGraceFailsCleanly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Rollback(ctx, nil, now)
	require.Error(t, err)
	assert.True(t, IsJwksState(err))
	assert.Contains(t, err.Error(), "no grace key")
}

func TestServiceSignActiveReturnsDeterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	digest := []byte("hello")
	out, err := s.SignActive(ctx, digest)
	require.NoError(t, err)
	assert.Equal(t, "k-v1", out.Kid)
	assert.Equal(t, VaultKeyRef{Name: "k", Version: 1}, out.Key)
	// Fake signer prefixes "<kid>:" then echoes the digest.
	assert.Equal(t, []byte("k-v1:hello"), out.Signature)
}

func TestServiceActiveSigningKeyAfterRotate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewService(NewInMemoryStore(), NewFakeTransitKeyClient("k"), ASVSL2Default)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Rotate(ctx, now)
	require.NoError(t, err)
	kid, ref, err := s.ActiveSigningKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, "k-v2", kid)
	assert.Equal(t, VaultKeyRef{Name: "k", Version: 2}, ref)
}

// --- Failure-path integration: vault errors propagate as ErrJwksVault ---

type failingTransit struct {
	*FakeTransitKeyClient
	failOn string
}

func (f *failingTransit) RotateKey(ctx context.Context) (VaultKeyRef, error) {
	if f.failOn == "rotate" {
		return VaultKeyRef{}, fmt.Errorf("vault unavailable")
	}
	return f.FakeTransitKeyClient.RotateKey(ctx)
}

func TestServiceRotateSurfacesVaultError(t *testing.T) {
	t.Parallel()
	transit := &failingTransit{FakeTransitKeyClient: NewFakeTransitKeyClient("k"), failOn: "rotate"}
	s := NewService(NewInMemoryStore(), transit, ASVSL2Default)
	_, err := s.Rotate(context.Background(), time.Now().UTC())
	require.Error(t, err)
	assert.True(t, IsJwksVault(err), "vault errors must classify as ErrJwksVault")
}

// Compile-time interface assertions.
var _ JwksKeyStore = (*InMemoryJwksKeyStore)(nil)
var _ TransitKeyClient = (*FakeTransitKeyClient)(nil)
