// Package grpcserver implements media_set.MediaSetService — the gRPC
// surface defined in proto/media_set/media_set_service.proto.
//
// Each RPC delegates to the same service-layer ops the REST handlers
// use, so behaviour stays in lockstep without a second copy of the
// SQL. Mirrors services/media-sets-service/src/grpc.rs verbatim.
package grpcserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	commonpb "github.com/openfoundry/openfoundry-go/libs/proto-gen/common"
	pb "github.com/openfoundry/openfoundry-go/libs/proto-gen/media_set"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediaitems"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/transactions"
)

// Service implements pb.MediaSetServiceServer.
//
// Audit context for every RPC mutation is synthesised here as the
// "grpc" actor with a fresh request-id (the Rust impl does the same;
// the real per-call principal lands when the gRPC layer learns to
// extract claims from gRPC metadata).
type Service struct {
	pb.UnimplementedMediaSetServiceServer
	Repo  *repo.Repo
	Items *mediaitems.Service
	Txs   *transactions.Service
}

// New builds a gRPC service handle. The REST + gRPC layers share the
// same service objects so audit / Cedar / metrics behave identically.
func New(r *repo.Repo, items *mediaitems.Service, txs *transactions.Service) *Service {
	return &Service{Repo: r, Items: items, Txs: txs}
}

// Register installs the service on a grpc.ServiceRegistrar.
func Register(s grpc.ServiceRegistrar, svc *Service) {
	pb.RegisterMediaSetServiceServer(s, svc)
}

// grpcCtx mirrors Rust grpc_ctx(): synthetic actor + request id.
func grpcCtx() audittrail.AuditContext {
	return audittrail.AuditContext{
		ActorID:       "grpc",
		RequestID:     uuid.New().String(),
		SourceService: "media-sets-service",
	}
}

// grpcClaims is the synthetic principal used while the gRPC surface
// has not yet wired metadata-based authn. The Sub is a deterministic
// nil UUID; tenant defaults to empty so Cedar `principal.tenant ==
// resource.tenant` enforces strict equality.
func grpcClaims() *authmw.Claims {
	return &authmw.Claims{
		Sub:          uuid.Nil,
		Roles:        []string{"editor"},
		SessionScope: &authmw.SessionScope{},
	}
}

// ── Media sets ────────────────────────────────────────────────────

func (s *Service) CreateMediaSet(ctx context.Context, in *pb.CreateMediaSetRequest) (*pb.MediaSet, error) {
	schema, err := schemaFromProto(in.GetSchema())
	if err != nil {
		return nil, err
	}
	policy := policyFromProto(in.GetTransactionPolicy())
	body := &models.CreateMediaSetRequest{
		ProjectRID:        in.GetProjectRid(),
		Name:              in.GetName(),
		Schema:            schema,
		AllowedMimeTypes:  in.GetAllowedMimeTypes(),
		TransactionPolicy: stringPtr(policy),
		RetentionSeconds:  int64Ptr(in.GetRetentionSeconds()),
		Virtual:           boolPtr(in.GetVirtual()),
		Markings:          in.GetMarkings(),
	}
	if rid := in.GetSourceRid(); rid != "" {
		body.SourceRID = stringPtr(rid)
	}
	row, err := s.Repo.CreateMediaSet(ctx, body, "grpc")
	if err != nil {
		return nil, statusFromErr(err)
	}
	return mediaSetToProto(row), nil
}

func (s *Service) GetMediaSet(ctx context.Context, in *pb.GetMediaSetRequest) (*pb.MediaSet, error) {
	row, err := s.Repo.GetMediaSet(ctx, in.GetRid())
	if err != nil {
		return nil, statusFromErr(err)
	}
	if row == nil {
		return nil, status.Error(codes.NotFound, in.GetRid())
	}
	return mediaSetToProto(row), nil
}

func (s *Service) ListMediaSets(ctx context.Context, in *pb.ListMediaSetsRequest) (*pb.ListMediaSetsResponse, error) {
	page, perPage := pageArgs(in.GetPagination())
	rows, err := s.Repo.ListMediaSets(ctx, in.GetProjectRid())
	if err != nil {
		return nil, statusFromErr(err)
	}
	out := make([]*pb.MediaSet, 0, len(rows))
	for i := range rows {
		out = append(out, mediaSetToProto(&rows[i]))
	}
	return &pb.ListMediaSetsResponse{
		MediaSets: out,
		Pagination: &commonpb.PageResponse{
			Page: page, PerPage: perPage,
			Total: int64(len(rows)), TotalPages: 1,
		},
	}, nil
}

func (s *Service) DeleteMediaSet(ctx context.Context, in *pb.DeleteMediaSetRequest) (*pb.DeleteMediaSetResponse, error) {
	deleted, err := s.Repo.DeleteMediaSet(ctx, in.GetRid())
	if err != nil {
		return nil, statusFromErr(err)
	}
	if !deleted {
		return nil, status.Error(codes.NotFound, in.GetRid())
	}
	return &pb.DeleteMediaSetResponse{}, nil
}

// ── Transactions ─────────────────────────────────────────────────

func (s *Service) OpenTransaction(ctx context.Context, in *pb.OpenTransactionRequest) (*pb.Transaction, error) {
	branch := in.GetBranch()
	if branch == "" {
		branch = "main"
	}
	// gRPC does not expose write_mode yet; default to MODIFY (matches
	// the Rust comment about REPLACE remaining REST-only).
	mode := models.WriteModeModify
	row, err := s.Txs.Open(ctx, transactions.OpenInput{
		MediaSetRID: in.GetMediaSetRid(),
		Body: models.OpenTransactionRequest{
			Branch:    &branch,
			WriteMode: &mode,
		},
		Claims:   grpcClaims(),
		AuditCtx: grpcCtx(),
	})
	if err != nil {
		return nil, statusFromTransactionErr(err)
	}
	return transactionToProto(row), nil
}

func (s *Service) CommitTransaction(ctx context.Context, in *pb.CommitTransactionRequest) (*pb.Transaction, error) {
	row, err := s.Txs.Commit(ctx, transactions.CloseInput{
		RID: in.GetTransactionRid(), Claims: grpcClaims(), AuditCtx: grpcCtx(),
	})
	if err != nil {
		return nil, statusFromTransactionErr(err)
	}
	return transactionToProto(row), nil
}

func (s *Service) AbortTransaction(ctx context.Context, in *pb.AbortTransactionRequest) (*pb.Transaction, error) {
	row, err := s.Txs.Abort(ctx, transactions.CloseInput{
		RID: in.GetTransactionRid(), Claims: grpcClaims(), AuditCtx: grpcCtx(),
	})
	if err != nil {
		return nil, statusFromTransactionErr(err)
	}
	return transactionToProto(row), nil
}

// ── Media items ──────────────────────────────────────────────────

func (s *Service) ListMediaItems(ctx context.Context, in *pb.ListMediaItemsRequest) (*pb.ListMediaItemsResponse, error) {
	branch := in.GetBranch()
	if branch == "" {
		branch = "main"
	}
	page, perPage := pageArgs(in.GetPagination())
	prefix := in.GetPathPrefix()
	rows, err := s.Items.List(ctx, mediaitems.ListInput{
		MediaSetRID: in.GetMediaSetRid(),
		Branch:      branch,
		Prefix:      stringOrNil(prefix),
		Limit:       int(perPage),
		Claims:      grpcClaims(),
	})
	if err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	out := make([]*pb.MediaItem, 0, len(rows))
	for i := range rows {
		out = append(out, mediaItemToProto(&rows[i]))
	}
	return &pb.ListMediaItemsResponse{
		Items: out,
		Pagination: &commonpb.PageResponse{
			Page: page, PerPage: perPage,
			Total: int64(len(rows)), TotalPages: 1,
		},
	}, nil
}

func (s *Service) GetMediaItem(ctx context.Context, in *pb.GetMediaItemRequest) (*pb.MediaItem, error) {
	row, err := s.Items.Get(ctx, grpcClaims(), in.GetRid())
	if err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	return mediaItemToProto(row), nil
}

func (s *Service) DeleteMediaItem(ctx context.Context, in *pb.DeleteMediaItemRequest) (*pb.DeleteMediaItemResponse, error) {
	if err := s.Items.Delete(ctx, mediaitems.DeleteInput{
		ItemRID: in.GetRid(), Claims: grpcClaims(), AuditCtx: grpcCtx(),
	}); err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	return &pb.DeleteMediaItemResponse{}, nil
}

// ── Presigned URLs ───────────────────────────────────────────────

func (s *Service) GeneratePresignedUploadUrl(ctx context.Context, in *pb.GeneratePresignedUploadUrlRequest) (*pb.PresignedUrlResponse, error) {
	branch := in.GetBranch()
	if branch == "" {
		branch = "main"
	}
	body := models.PresignedUploadRequest{
		Path:     in.GetPath(),
		MimeType: in.GetMimeType(),
		Branch:   stringPtr(branch),
	}
	if t := in.GetTransactionRid(); t != "" {
		body.TransactionRID = stringPtr(t)
	}
	if e := in.GetExpiresInSeconds(); e > 0 {
		v := uint64(e)
		body.ExpiresInSeconds = &v
	}
	res, err := s.Items.PresignUpload(ctx, mediaitems.PresignUploadInput{
		MediaSetRID: in.GetMediaSetRid(),
		Body:        body,
		Claims:      grpcClaims(),
		AuditCtx:    grpcCtx(),
	})
	if err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	return presignedToProto(res.URL.URL, res.URL.ExpiresAt.Unix(), res.URL.ExpiresAt.Nanosecond(), headerSliceToMap(res.URL.Headers)), nil
}

func (s *Service) GeneratePresignedDownloadUrl(ctx context.Context, in *pb.GeneratePresignedDownloadUrlRequest) (*pb.PresignedUrlResponse, error) {
	var ttl *uint64
	if e := in.GetExpiresInSeconds(); e > 0 {
		v := uint64(e)
		ttl = &v
	}
	res, err := s.Items.PresignDownload(ctx, mediaitems.PresignDownloadInput{
		ItemRID:          in.GetMediaItemRid(),
		ExpiresInSeconds: ttl,
		Claims:           grpcClaims(),
		AuditCtx:         grpcCtx(),
	})
	if err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	return presignedToProto(res.URL.URL, res.URL.ExpiresAt.Unix(), res.URL.ExpiresAt.Nanosecond(), nil), nil
}

func (s *Service) RegisterVirtualMediaItem(ctx context.Context, in *pb.RegisterVirtualMediaItemRequest) (*pb.MediaItem, error) {
	body := models.RegisterVirtualItemRequest{
		PhysicalPath: in.GetPhysicalPath(),
		ItemPath:     in.GetItemPath(),
	}
	if b := in.GetBranch(); b != "" {
		body.Branch = stringPtr(b)
	}
	if m := in.GetMimeType(); m != "" {
		body.MimeType = stringPtr(m)
	}
	if sz := in.GetSizeBytes(); sz > 0 {
		body.SizeBytes = int64Ptr(sz)
	}
	if sha := in.GetSha256(); sha != "" {
		body.SHA256 = stringPtr(sha)
	}
	row, err := s.Items.RegisterVirtual(ctx, mediaitems.RegisterVirtualInput{
		MediaSetRID: in.GetMediaSetRid(), Body: body,
		Claims: grpcClaims(), AuditCtx: grpcCtx(),
	})
	if err != nil {
		return nil, statusFromMediaItemErr(err)
	}
	return mediaItemToProto(row), nil
}

// ── Conversions ───────────────────────────────────────────────────

func mediaSetToProto(row *models.MediaSet) *pb.MediaSet {
	out := &pb.MediaSet{
		Rid:               row.RID,
		Name:              row.Name,
		ProjectRid:        row.ProjectRID,
		Schema:            schemaStrToProto(row.Schema),
		AllowedMimeTypes:  row.AllowedMimeTypes,
		TransactionPolicy: policyStrToProto(row.TransactionPolicy),
		RetentionSeconds:  row.RetentionSeconds,
		Virtual:           row.Virtual,
		Markings:          row.Markings,
		CreatedAt:         timestamppb.New(row.CreatedAt),
		CreatedBy:         row.CreatedBy,
	}
	if row.SourceRID != nil {
		v := *row.SourceRID
		out.SourceRid = &v
	}
	return out
}

func mediaItemToProto(row *models.MediaItem) *pb.MediaItem {
	meta := string(row.Metadata)
	if meta == "" {
		meta = "{}"
	}
	out := &pb.MediaItem{
		Rid:            row.RID,
		MediaSetRid:    row.MediaSetRID,
		Branch:         row.Branch,
		TransactionRid: row.TransactionRID,
		Path:           row.Path,
		MimeType:       row.MimeType,
		SizeBytes:      row.SizeBytes,
		Sha256:         row.SHA256,
		Metadata:       meta,
		CreatedAt:      timestamppb.New(row.CreatedAt),
	}
	if row.DeduplicatedFrom != nil {
		v := *row.DeduplicatedFrom
		out.DeduplicatedFrom = &v
	}
	return out
}

func transactionToProto(row *models.MediaSetTransaction) *pb.Transaction {
	out := &pb.Transaction{
		Rid:         row.RID,
		MediaSetRid: row.MediaSetRID,
		Branch:      row.Branch,
		State:       txnStateStrToProto(row.State),
		OpenedAt:    timestamppb.New(row.OpenedAt),
		OpenedBy:    row.OpenedBy,
	}
	if row.ClosedAt != nil {
		out.ClosedAt = timestamppb.New(*row.ClosedAt)
	}
	return out
}

func presignedToProto(url string, sec int64, nano int, headers map[string]string) *pb.PresignedUrlResponse {
	return &pb.PresignedUrlResponse{
		Url:       url,
		Headers:   headers,
		ExpiresAt: &timestamppb.Timestamp{Seconds: sec, Nanos: int32(nano)},
	}
}

func headerSliceToMap(in []storage.HeaderPair) map[string]string {
	out := make(map[string]string, len(in))
	for _, p := range in {
		out[p.Name] = p.Value
	}
	return out
}

// schemaFromProto rejects UNSPECIFIED with InvalidArgument.
func schemaFromProto(s pb.MediaSetSchema) (string, error) {
	switch s {
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_IMAGE:
		return "IMAGE", nil
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_AUDIO:
		return "AUDIO", nil
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_VIDEO:
		return "VIDEO", nil
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_DOCUMENT:
		return "DOCUMENT", nil
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_SPREADSHEET:
		return "SPREADSHEET", nil
	case pb.MediaSetSchema_MEDIA_SET_SCHEMA_EMAIL:
		return "EMAIL", nil
	}
	return "", status.Error(codes.InvalidArgument, "schema is required")
}

func policyFromProto(p pb.TransactionPolicy) string {
	if p == pb.TransactionPolicy_TRANSACTION_POLICY_TRANSACTIONAL {
		return "TRANSACTIONAL"
	}
	return "TRANSACTIONLESS"
}

func schemaStrToProto(s string) pb.MediaSetSchema {
	switch s {
	case "IMAGE":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_IMAGE
	case "AUDIO":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_AUDIO
	case "VIDEO":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_VIDEO
	case "DOCUMENT":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_DOCUMENT
	case "SPREADSHEET":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_SPREADSHEET
	case "EMAIL":
		return pb.MediaSetSchema_MEDIA_SET_SCHEMA_EMAIL
	}
	return pb.MediaSetSchema_MEDIA_SET_SCHEMA_UNSPECIFIED
}

func policyStrToProto(s string) pb.TransactionPolicy {
	switch s {
	case "TRANSACTIONAL":
		return pb.TransactionPolicy_TRANSACTION_POLICY_TRANSACTIONAL
	case "TRANSACTIONLESS":
		return pb.TransactionPolicy_TRANSACTION_POLICY_TRANSACTIONLESS
	}
	return pb.TransactionPolicy_TRANSACTION_POLICY_UNSPECIFIED
}

func txnStateStrToProto(s string) pb.TransactionState {
	switch s {
	case "OPEN":
		return pb.TransactionState_TRANSACTION_STATE_OPEN
	case "COMMITTED":
		return pb.TransactionState_TRANSACTION_STATE_COMMITTED
	case "ABORTED":
		return pb.TransactionState_TRANSACTION_STATE_ABORTED
	}
	return pb.TransactionState_TRANSACTION_STATE_UNSPECIFIED
}

func pageArgs(p *commonpb.PageRequest) (int32, int32) {
	page, per := int32(1), int32(50)
	if p != nil {
		if p.Page > 0 {
			page = p.Page
		}
		if p.PerPage > 0 {
			per = p.PerPage
		}
	}
	return page, per
}

func stringPtr(s string) *string { return &s }
func int64Ptr(v int64) *int64    { return &v }
func boolPtr(b bool) *bool       { return &b }

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ── Error → grpc.Status mapping ──────────────────────────────────

// statusFromErr maps generic SQL-layer errors to internal/InvalidArg.
func statusFromErr(err error) error {
	return status.Errorf(codes.Internal, "%v", err)
}

// statusFromTransactionErr surfaces the typed transactions errors.
func statusFromTransactionErr(err error) error {
	switch e := err.(type) {
	case *transactions.ErrBadRequest:
		return status.Error(codes.InvalidArgument, e.Msg)
	case *transactions.ErrNotFound:
		return status.Error(codes.NotFound, e.Error())
	case *transactions.ErrTransactionless:
		return status.Error(codes.FailedPrecondition, e.Error())
	case *transactions.ErrTransactionTerminal:
		return status.Error(codes.FailedPrecondition, e.Error())
	case *transactions.ErrTransactionConflict:
		return status.Error(codes.Aborted, e.Error())
	default:
		return statusFromErr(err)
	}
}

// statusFromMediaItemErr surfaces the typed mediaitems errors.
func statusFromMediaItemErr(err error) error {
	switch e := err.(type) {
	case *mediaitems.ErrBadRequest:
		return status.Error(codes.InvalidArgument, e.Msg)
	case *mediaitems.ErrNotFound:
		return status.Error(codes.NotFound, e.Error())
	default:
		return statusFromErr(err)
	}
}

// debugProto stringifies a proto message for error logging without
// depending on prototext (which would add a runtime allocator).
//
//nolint:unused // kept for future structured-error wrapping
func debugProto(m fmt.Stringer) string {
	if m == nil {
		return ""
	}
	b, _ := json.Marshal(struct{ Msg string }{m.String()})
	return string(b)
}
