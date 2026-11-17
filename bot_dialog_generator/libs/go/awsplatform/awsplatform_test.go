package awsplatform

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func generateTestCert(t *testing.T) (caPEM []byte, serverTLS tls.Certificate) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aws-emulator-local"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(caPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return caPEM, cert
}

func TestParseStartupLine(t *testing.T) {
	logStream := `{"level":"INFO","message":"starting up"}
{"message":"emulator server information","endpoint_url":"https://127.0.0.1:9297","health_check_url":"https://127.0.0.1:9297/__admin/health","config_file":"/home/emulator/.aws/emulator/9297/config","ca_file":"/home/emulator/.aws/emulator/9297/ca.pem","java_truststore_file":"/home/emulator/.aws/emulator/9297/truststore.p12"}
`
	info, err := ParseStartupLine(strings.NewReader(logStream))
	if err != nil {
		t.Fatal(err)
	}
	if info.EndpointURL != "https://127.0.0.1:9297" || info.HealthCheckURL != "https://127.0.0.1:9297/__admin/health" {
		t.Fatalf("unexpected startup info: %+v", info)
	}
}

func TestEmulatorHTTPSConfigHealthAndSTS(t *testing.T) {
	caPEM, serverCert := generateTestCert(t)

	var sawSigV4 bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__admin/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if strings.Contains(r.Header.Get("Authorization"), "Credential=EMULATOR0123456789AB/") {
			sawSigV4 = true
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>arn:aws:iam::000000000000:user/emulator</Arn>
    <UserId>EMULATOR0123456789AB</UserId>
    <Account>000000000000</Account>
  </GetCallerIdentityResult>
</GetCallerIdentityResponse>`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}}
	srv.StartTLS()
	defer srv.Close()

	port := extractPort(srv.URL)
	fakeHome := t.TempDir()
	portDir := filepath.Join(fakeHome, ".aws", "emulator", port)
	if err := os.MkdirAll(portDir, 0o700); err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(portDir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	credsPath := filepath.Join(portDir, "emulator_credentials")
	credsContent := "[default]\naws_access_key_id = EMULATOR0123456789AB\naws_secret_access_key = 0123456789ABCDEF0123456789ABCDEF01234567\n"
	if err := os.WriteFile(credsPath, []byte(credsContent), 0o600); err != nil {
		t.Fatal(err)
	}
	// Simulate Docker on Windows writing a container-internal path in config.
	cfgPath := filepath.Join(portDir, "config")
	cfgContent := "[default]\nregion = us-east-1\nendpoint_url = " + srv.URL + "\nca_bundle = /home/emulator/.aws/emulator/" + port + "/ca.pem\n"
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}

	getenv := func(k string) string {
		switch k {
		case "HOME", "USERPROFILE":
			return fakeHome
		case "AWS_ENDPOINT_URL":
			return srv.URL
		default:
			return ""
		}
	}

	cfg, opts, err := LoadConfig(context.Background(), getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.DisableHostPrefixInjection || !opts.EC2MetadataDisabled {
		t.Fatalf("expected host prefix injection and EC2 metadata to be disabled for emulator: %+v", opts)
	}
	if err := CheckHealth(context.Background(), opts.EndpointURL, opts.CABundleFile); err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	idOut, err := VerifyEmulatorIdentity(context.Background(), cfg)
	if err != nil {
		t.Fatalf("VerifyEmulatorIdentity failed: %v", err)
	}
	if aws.ToString(idOut.Account) != EmulatorAccountID || aws.ToString(idOut.Arn) != EmulatorUserARN {
		t.Fatalf("unexpected identity: %+v", idOut)
	}
	if !sawSigV4 {
		t.Fatal("expected request to be signed with EMULATOR0123456789AB credentials")
	}
}

// --- In-memory fakes for S3, KMS, SQS, and Secrets Manager unit tests ---

type fakeKMS struct {
	keys    map[string]kmstypes.KeyUsageType
	aliases map[string]string
	secret  []byte
}

func newFakeKMS() *fakeKMS {
	return &fakeKMS{
		keys:    map[string]kmstypes.KeyUsageType{},
		aliases: map[string]string{},
		secret:  []byte("test-kms-signing-secret"),
	}
}

func (f *fakeKMS) CreateKey(_ context.Context, in *kms.CreateKeyInput, _ ...func(*kms.Options)) (*kms.CreateKeyOutput, error) {
	id := fmt.Sprintf("key-%d", len(f.keys)+1)
	f.keys[id] = in.KeyUsage
	return &kms.CreateKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String(id), KeyUsage: in.KeyUsage}}, nil
}

func (f *fakeKMS) CreateAlias(_ context.Context, in *kms.CreateAliasInput, _ ...func(*kms.Options)) (*kms.CreateAliasOutput, error) {
	f.aliases[aws.ToString(in.AliasName)] = aws.ToString(in.TargetKeyId)
	return &kms.CreateAliasOutput{}, nil
}

func (f *fakeKMS) DescribeKey(_ context.Context, in *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	k := aws.ToString(in.KeyId)
	if target, ok := f.aliases[k]; ok {
		return &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String(target)}}, nil
	}
	if _, ok := f.keys[k]; ok {
		return &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String(k)}}, nil
	}
	return nil, errors.New("NotFoundException")
}

func (f *fakeKMS) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(aws.ToString(in.KeyId)))
	mac.Write(in.Message)
	return &kms.SignOutput{KeyId: in.KeyId, Signature: mac.Sum(nil)}, nil
}

func (f *fakeKMS) Verify(ctx context.Context, in *kms.VerifyInput, _ ...func(*kms.Options)) (*kms.VerifyOutput, error) {
	sigOut, _ := f.Sign(ctx, &kms.SignInput{KeyId: in.KeyId, Message: in.Message})
	valid := hmac.Equal(sigOut.Signature, in.Signature)
	return &kms.VerifyOutput{KeyId: in.KeyId, SignatureValid: valid}, nil
}

func (f *fakeKMS) Encrypt(_ context.Context, in *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	cipher := append([]byte("enc:"), in.Plaintext...)
	return &kms.EncryptOutput{KeyId: in.KeyId, CiphertextBlob: cipher}, nil
}

func (f *fakeKMS) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	if !bytes.HasPrefix(in.CiphertextBlob, []byte("enc:")) {
		return nil, errors.New("InvalidCiphertextException")
	}
	return &kms.DecryptOutput{Plaintext: bytes.TrimPrefix(in.CiphertextBlob, []byte("enc:"))}, nil
}

type storedS3Obj struct {
	body        []byte
	contentType string
	metadata    map[string]string
	versionID   string
}

type fakeS3 struct {
	buckets map[string]map[string][]storedS3Obj
	seq     int
}

func newFakeS3() *fakeS3 {
	return &fakeS3{buckets: map[string]map[string][]storedS3Obj{}}
}

func (f *fakeS3) CreateBucket(_ context.Context, in *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	b := aws.ToString(in.Bucket)
	if _, ok := f.buckets[b]; !ok {
		f.buckets[b] = map[string][]storedS3Obj{}
	}
	return &s3.CreateBucketOutput{}, nil
}

func (f *fakeS3) HeadBucket(_ context.Context, in *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if _, ok := f.buckets[aws.ToString(in.Bucket)]; !ok {
		return nil, &s3types.NotFound{}
	}
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeS3) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b := aws.ToString(in.Bucket)
	if _, ok := f.buckets[b]; !ok {
		f.buckets[b] = map[string][]storedS3Obj{}
	}
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.seq++
	verID := fmt.Sprintf("ver-%d", f.seq)
	meta := map[string]string{}
	for k, v := range in.Metadata {
		meta[k] = v
	}
	key := aws.ToString(in.Key)
	f.buckets[b][key] = append(f.buckets[b][key], storedS3Obj{
		body:        data,
		contentType: aws.ToString(in.ContentType),
		metadata:    meta,
		versionID:   verID,
	})
	return &s3.PutObjectOutput{VersionId: aws.String(verID)}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	b := aws.ToString(in.Bucket)
	key := aws.ToString(in.Key)
	vers := f.buckets[b][key]
	if len(vers) == 0 {
		return nil, &s3types.NoSuchKey{}
	}
	chosen := vers[len(vers)-1]
	if wantVer := aws.ToString(in.VersionId); wantVer != "" {
		found := false
		for _, v := range vers {
			if v.versionID == wantVer {
				chosen = v
				found = true
				break
			}
		}
		if !found {
			return nil, &s3types.NoSuchKey{}
		}
	}
	return &s3.GetObjectOutput{
		Body:        io.NopCloser(bytes.NewReader(chosen.body)),
		ContentType: aws.String(chosen.contentType),
		Metadata:    chosen.metadata,
		VersionId:   aws.String(chosen.versionID),
	}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.buckets[aws.ToString(in.Bucket)], aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) CopyObject(_ context.Context, in *s3.CopyObjectInput, _ ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	srcBucket, srcKey, ok := strings.Cut(aws.ToString(in.CopySource), "/")
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	vers := f.buckets[srcBucket][srcKey]
	if len(vers) == 0 {
		return nil, &s3types.NoSuchKey{}
	}
	latest := vers[len(vers)-1]
	b := aws.ToString(in.Bucket)
	dstKey := aws.ToString(in.Key)
	f.seq++
	verID := fmt.Sprintf("ver-%d", f.seq)
	f.buckets[b][dstKey] = append(f.buckets[b][dstKey], storedS3Obj{
		body:        append([]byte(nil), latest.body...),
		contentType: latest.contentType,
		metadata:    latest.metadata,
		versionID:   verID,
	})
	return &s3.CopyObjectOutput{}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	b := aws.ToString(in.Bucket)
	prefix := aws.ToString(in.Prefix)
	var contents []s3types.Object
	for k, vers := range f.buckets[b] {
		if strings.HasPrefix(k, prefix) && len(vers) > 0 {
			latest := vers[len(vers)-1]
			contents = append(contents, s3types.Object{
				Key:  aws.String(k),
				Size: aws.Int64(int64(len(latest.body))),
			})
		}
	}
	return &s3.ListObjectsV2Output{Contents: contents}, nil
}

func (f *fakeS3) ListObjectVersions(_ context.Context, in *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	b := aws.ToString(in.Bucket)
	prefix := aws.ToString(in.Prefix)
	var versions []s3types.ObjectVersion
	for k, vers := range f.buckets[b] {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		for i, v := range vers {
			versions = append(versions, s3types.ObjectVersion{
				Key:       aws.String(k),
				VersionId: aws.String(v.versionID),
				IsLatest:  aws.Bool(i == len(vers)-1),
				Size:      aws.Int64(int64(len(v.body))),
			})
		}
	}
	return &s3.ListObjectVersionsOutput{Versions: versions}, nil
}

func TestS3AndKMSMigratedServices(t *testing.T) {
	ctx := context.Background()
	kmsSvc := NewKMSService(newFakeKMS())
	keyID, err := kmsSvc.EnsureSigningKey(ctx, "alias/bot-dialog-generator-signing")
	if err != nil {
		t.Fatal(err)
	}
	wsKey, err := kmsSvc.EnsureWorkspaceKey(ctx, "ws-hr")
	if err != nil || wsKey == "" {
		t.Fatalf("EnsureWorkspaceKey: key=%q err=%v", wsKey, err)
	}

	// KMS encrypt/decrypt (migrated from F:\artemis\bot_engine\src\utils.py decrypt)
	cipher, err := kmsSvc.Encrypt(ctx, wsKey, []byte("secret-account-42"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := kmsSvc.Decrypt(ctx, cipher)
	if err != nil || string(plain) != "secret-account-42" {
		t.Fatalf("Decrypt = %q, err = %v", string(plain), err)
	}

	// S3 definition upload, KMS verification, and version rollback (pickle_rollback_lambda)
	s3Backend := newFakeS3()
	s3Svc := NewS3Service(s3Backend, kmsSvc, keyID)
	const bucket = "bot-dialog-generator-definitions"
	if err := s3Svc.EnsureBucket(ctx, bucket, true); err != nil {
		t.Fatal(err)
	}

	storedV1, err := s3Svc.PutDefinition(ctx, bucket, "ws-hr", "hr-helpdesk", "v1", []byte("proto-v1"))
	if err != nil {
		t.Fatal(err)
	}
	if storedV1.URI != "s3://bot-dialog-generator-definitions/workspaces/ws-hr/bots/hr-helpdesk/v1.pb" {
		t.Fatalf("URI = %q", storedV1.URI)
	}
	got, err := s3Svc.GetDefinition(ctx, bucket, "ws-hr", "hr-helpdesk", "v1", "")
	if err != nil || string(got.Bytes) != "proto-v1" {
		t.Fatalf("GetDefinition = %q, err = %v", string(got.Bytes), err)
	}

	// Tamper with stored payload in S3 and confirm KMS / SHA256 check rejects it
	s3Backend.buckets[bucket][storedV1.Key][0].body = []byte("tampered-proto")
	if _, err := s3Svc.GetDefinition(ctx, bucket, "ws-hr", "hr-helpdesk", "v1", ""); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("expected ErrSignatureInvalid on tampered definition, got %v", err)
	}

	// Version listing (migrated from pickle_rollback_lambda)
	vers, err := s3Svc.ListDefinitionVersions(ctx, bucket, "ws-hr", "hr-helpdesk", 10)
	if err != nil || len(vers) != 1 {
		t.Fatalf("ListDefinitionVersions = %+v, err = %v", vers, err)
	}

	// Path traversal / cross-workspace key injection is rejected
	if _, err := s3Svc.PutDefinition(ctx, bucket, "../ws-other", "hr-helpdesk", "v1", []byte("x")); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}

	// Multimedia upload, WebChat/Teams normalization, rename, list, and size check (F:\artemis back + media_admin)
	const mediaBucket = "bot-dialog-generator-media"
	obj, err := s3Svc.UploadMultimedia(ctx, mediaBucket, "ws-hr", "hr-helpdesk", "WebChat/Teams", "image", "node1.png", "image/png", []byte("png-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.Channel != "webchat-teams" || obj.Key != "workspaces/ws-hr/bots/hr-helpdesk/media/webchat-teams/image/node1.png" {
		t.Fatalf("unexpected multimedia object: %+v", obj)
	}
	renamed, err := s3Svc.RenameMultimedia(ctx, mediaBucket, "ws-hr", "hr-helpdesk", "WebChat/Teams", "image", "node1.png", "node2.png")
	if err != nil || renamed.FileName != "node2.png" {
		t.Fatalf("RenameMultimedia: %+v, err=%v", renamed, err)
	}
	if _, err := s3Svc.GetMultimedia(ctx, mediaBucket, "ws-hr", "hr-helpdesk", "WebChat/Teams", "image", "node1.png"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("old multimedia file should be deleted after rename, got %v", err)
	}
	loadedMedia, err := s3Svc.GetMultimedia(ctx, mediaBucket, "ws-hr", "hr-helpdesk", "WebChat/Teams", "image", "node2.png")
	if err != nil || string(loadedMedia.Body) != "png-bytes" {
		t.Fatalf("GetMultimedia after rename: %+v, err=%v", loadedMedia, err)
	}
	mediaList, err := s3Svc.ListMultimedia(ctx, mediaBucket, "ws-hr", "hr-helpdesk")
	if err != nil || len(mediaList) != 1 || !ValidateMediaSize(mediaList, 1024) {
		t.Fatalf("ListMultimedia = %+v, err = %v", mediaList, err)
	}

	// Campaign file lifecycle (migrated from F:\artemis back/campaigns.service.ts & ingester_lambda)
	uri, err := s3Svc.UploadCampaignFile(ctx, "bot-dialog-generator-campaigns", "ws-hr", "contacts.csv", []byte("id,phone\n1,555"))
	if err != nil || uri != "s3://bot-dialog-generator-campaigns/workspaces/ws-hr/campaigns/contacts.csv" {
		t.Fatalf("UploadCampaignFile uri=%q err=%v", uri, err)
	}
	csvBytes, err := s3Svc.GetCampaignFile(ctx, "bot-dialog-generator-campaigns", "ws-hr", "contacts.csv")
	if err != nil || string(csvBytes) != "id,phone\n1,555" {
		t.Fatalf("GetCampaignFile = %q, err = %v", string(csvBytes), err)
	}
	if err := s3Svc.DeleteCampaignFile(ctx, "bot-dialog-generator-campaigns", "ws-hr", "contacts.csv"); err != nil {
		t.Fatal(err)
	}
}

type fakeSQS struct {
	queues map[string][]sqstypes.Message
	seq    int
}

func newFakeSQS() *fakeSQS {
	return &fakeSQS{queues: map[string][]sqstypes.Message{}}
}

func (f *fakeSQS) CreateQueue(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	u := "https://127.0.0.1:9297/000000000000/" + aws.ToString(in.QueueName)
	if _, ok := f.queues[u]; !ok {
		f.queues[u] = nil
	}
	return &sqs.CreateQueueOutput{QueueUrl: aws.String(u)}, nil
}

func (f *fakeSQS) GetQueueUrl(_ context.Context, in *sqs.GetQueueUrlInput, _ ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	u := "https://127.0.0.1:9297/000000000000/" + aws.ToString(in.QueueName)
	if _, ok := f.queues[u]; !ok {
		return nil, errors.New("QueueDoesNotExist")
	}
	return &sqs.GetQueueUrlOutput{QueueUrl: aws.String(u)}, nil
}

func (f *fakeSQS) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.seq++
	id := fmt.Sprintf("msg-%d", f.seq)
	u := aws.ToString(in.QueueUrl)
	f.queues[u] = append(f.queues[u], sqstypes.Message{
		MessageId:         aws.String(id),
		ReceiptHandle:     aws.String("rh-" + id),
		Body:              in.MessageBody,
		MessageAttributes: in.MessageAttributes,
	})
	return &sqs.SendMessageOutput{MessageId: aws.String(id)}, nil
}

func (f *fakeSQS) SendMessageBatch(ctx context.Context, in *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	var ok []sqstypes.SendMessageBatchResultEntry
	for _, e := range in.Entries {
		out, _ := f.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:          in.QueueUrl,
			MessageBody:       e.MessageBody,
			MessageAttributes: e.MessageAttributes,
		})
		ok = append(ok, sqstypes.SendMessageBatchResultEntry{Id: e.Id, MessageId: out.MessageId})
	}
	return &sqs.SendMessageBatchOutput{Successful: ok}, nil
}

func (f *fakeSQS) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	u := aws.ToString(in.QueueUrl)
	msgs := f.queues[u]
	n := int(in.MaxNumberOfMessages)
	if n > len(msgs) {
		n = len(msgs)
	}
	return &sqs.ReceiveMessageOutput{Messages: append([]sqstypes.Message(nil), msgs[:n]...)}, nil
}

func (f *fakeSQS) DeleteMessageBatch(_ context.Context, in *sqs.DeleteMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	u := aws.ToString(in.QueueUrl)
	drop := map[string]bool{}
	var ok []sqstypes.DeleteMessageBatchResultEntry
	for _, e := range in.Entries {
		drop[aws.ToString(e.ReceiptHandle)] = true
		ok = append(ok, sqstypes.DeleteMessageBatchResultEntry{Id: e.Id})
	}
	var kept []sqstypes.Message
	for _, m := range f.queues[u] {
		if !drop[aws.ToString(m.ReceiptHandle)] {
			kept = append(kept, m)
		}
	}
	f.queues[u] = kept
	return &sqs.DeleteMessageBatchOutput{Successful: ok}, nil
}

func TestSQSMigratedService(t *testing.T) {
	ctx := context.Background()
	sqsSvc := NewSQSService(newFakeSQS())
	qURL, err := sqsSvc.EnsureQueue(ctx, "bot-dialog-generator-campaigns")
	if err != nil {
		t.Fatal(err)
	}

	// Chunked batch send (>10 items, migrating scheduler_lambda QueueManagement.send_messages)
	var batch []any
	for i := range 15 {
		batch = append(batch, map[string]any{"recipient": i})
	}
	res, err := sqsSvc.SendBatch(ctx, qURL, "ws-customer-service", batch)
	if err != nil || res.Successful != 15 || res.Failed != 0 {
		t.Fatalf("SendBatch = %+v, err = %v", res, err)
	}

	// Receive and delete batch (migrating campaigns QueueService.get_messages & delete_messages)
	msgs, err := sqsSvc.ReceiveMessages(ctx, qURL, 10, 0)
	if err != nil || len(msgs) != 10 || msgs[0].WorkspaceID != "ws-customer-service" {
		t.Fatalf("ReceiveMessages = %+v, err = %v", msgs, err)
	}
	delRes, err := sqsSvc.DeleteMessages(ctx, qURL, msgs)
	if err != nil || delRes.Successful != 10 {
		t.Fatalf("DeleteMessages = %+v, err = %v", delRes, err)
	}
	remaining, err := sqsSvc.ReceiveMessages(ctx, qURL, 10, 0)
	if err != nil || len(remaining) != 5 {
		t.Fatalf("remaining messages = %d, want 5", len(remaining))
	}
}

type fakeSecrets struct {
	secrets map[string]string
}

func newFakeSecrets() *fakeSecrets {
	return &fakeSecrets{secrets: map[string]string{}}
}

func (f *fakeSecrets) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	val, ok := f.secrets[aws.ToString(in.SecretId)]
	if !ok {
		return nil, &smtypes.ResourceNotFoundException{}
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(val)}, nil
}

func (f *fakeSecrets) CreateSecret(_ context.Context, in *secretsmanager.CreateSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	name := aws.ToString(in.Name)
	if _, ok := f.secrets[name]; ok {
		return nil, &smtypes.ResourceExistsException{}
	}
	f.secrets[name] = aws.ToString(in.SecretString)
	return &secretsmanager.CreateSecretOutput{}, nil
}

func (f *fakeSecrets) PutSecretValue(_ context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	f.secrets[aws.ToString(in.SecretId)] = aws.ToString(in.SecretString)
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func TestSecretsManagerMigratedService(t *testing.T) {
	ctx := context.Background()
	smSvc := NewSecretsService(newFakeSecrets())
	want := DBSecret{
		Username: "studio_api",
		Password: "studio_api_local",
		Host:     "localhost",
		Port:     5433,
		DBName:   "bot_dialog_generator",
	}
	if err := smSvc.PutSecretJSON(ctx, "bdg/local/db", want); err != nil {
		t.Fatal(err)
	}
	got, err := smSvc.GetDBSecret(ctx, "bdg/local/db")
	if err != nil || got != want {
		t.Fatalf("GetDBSecret = %+v, err = %v", got, err)
	}
	if u := got.PostgresURL(); u != "postgres://studio_api:studio_api_local@localhost:5433/bot_dialog_generator" {
		t.Fatalf("PostgresURL = %q", u)
	}
}
