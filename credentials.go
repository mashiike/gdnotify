package gdnotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	logx "github.com/mashiike/go-logx"
	"google.golang.org/api/option"
)

type CredentialsBackend interface {
	WithCredentialsClientOption(context.Context, []option.ClientOption) ([]option.ClientOption, error)
}

func NewCredentialsBackend(ctx context.Context, cfg *CredentialsBackendConfig, awsCfg aws.Config) (CredentialsBackend, error) {
	switch cfg.BackendType {
	case CredentialsBackendTypeNone:
		return &NoneCredentialsBackend{}, nil
	case CredentialsBackendTypeSSMParameterStore:
		return NewSSMParameterStoreCredentialsBackend(ctx, cfg, awsCfg)
	}
	return nil, errors.New("unknown credentials backend type")
}

type NoneCredentialsBackend struct{}

func (b *NoneCredentialsBackend) WithCredentialsClientOption(_ context.Context, orig []option.ClientOption) ([]option.ClientOption, error) {
	return orig, nil
}

type SSMParameterStoreCredentialsBackend struct {
	client         *ssm.Client
	name           string
	base64Encoding bool
}

func NewSSMParameterStoreCredentialsBackend(ctx context.Context, cfg *CredentialsBackendConfig, awsCfg aws.Config) (*SSMParameterStoreCredentialsBackend, error) {
	return &SSMParameterStoreCredentialsBackend{
		client:         ssm.NewFromConfig(awsCfg),
		name:           *cfg.ParameterName,
		base64Encoding: cfg.Base64Encoding,
	}, nil
}

func (cb *SSMParameterStoreCredentialsBackend) WithCredentialsClientOption(ctx context.Context, orig []option.ClientOption) ([]option.ClientOption, error) {
	logx.Printf(ctx, "[debug] try get parameter name=%s", cb.name)
	output, err := cb.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(cb.name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		logx.Printf(ctx, "[debug] failed get parameter name=%s:%s", cb.name, err.Error())
		return orig, err
	}
	if output.Parameter == nil {
		logx.Printf(ctx, "[warn] get parameter from ssm name=%s, but empty", cb.name)
		return orig, err
	}
	var creds []byte
	if cb.base64Encoding {
		decoder := base64.NewDecoder(base64.RawStdEncoding, strings.NewReader(*output.Parameter.Value))
		var err error
		creds, err = io.ReadAll(decoder)
		if err != nil {
			logx.Printf(ctx, "[warn] credentials base64 decode failed:%s", err.Error())
			return orig, err
		}
	}
	if creds == nil {
		creds = []byte(*output.Parameter.Value)
	}
	var temp interface{}
	if err := json.Unmarshal(creds, &temp); err != nil {
		logx.Printf(ctx, "[debug] credentials is not json:%s", err.Error())
		return orig, fmt.Errorf("SSM Parameter `%s` loaded value is not json: %s", cb.name, err.Error())
	}
	ret := append(orig, option.WithCredentialsJSON(creds))
	return ret, nil
}
