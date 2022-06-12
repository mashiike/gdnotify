package gdnotify

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	lambdaapi "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/fujiwara/ridge"
	logx "github.com/mashiike/go-logx"
)

func isLambda() bool {
	if strings.HasPrefix(os.Getenv("AWS_EXECUTION_ENV"), "AWS_Lambda") || os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		return true
	}
	return false
}

type LambdaHandlerFunc func(context.Context, json.RawMessage) (interface{}, error)

func (app *App) LambdaHandler(opts *RunOptions) LambdaHandlerFunc {
	return func(ctx context.Context, event json.RawMessage) (interface{}, error) {
		logger := logx.Default(ctx)
		if opts.Mode == RunModeCleanup {
			logx.Println(ctx, "[error] cleanup run mode, can not run on lambda")
			return nil, errors.New("not supported")
		}
		if opts.Mode == RunModeChannelMaintenance {
			logx.Println(ctx, "[info] Handled as a channel maintenance event")
			return app.handleChannelMaintenance(
				logx.WithLogger(ctx, log.New(logger.Writer(), RunModeChannelMaintenance.String()+":", logger.Flags())),
				event,
			)
		}
		r, err := ridge.NewRequest(event)
		if err != nil {
			if opts.Mode == RunModeWebhook {
				logx.Println(ctx, err)
				return nil, err
			}
			logx.Println(ctx, "[info] Handled as a channel maintenance event")
			return app.handleChannelMaintenance(
				logx.WithLogger(ctx, log.New(logger.Writer(), RunModeChannelMaintenance.String()+":", logger.Flags())),
				event,
			)
		}
		w := ridge.NewResponseWriter()

		logx.Println(ctx, "[info] Handled as a webhook http event")
		r = r.WithContext(
			logx.WithLogger(ctx, log.New(logger.Writer(), RunModeWebhook.String()+":", logger.Flags())),
		)
		app.ServeHTTP(w, r)
		return w.Response(), nil
	}
}

func (app *App) handleChannelMaintenance(ctx context.Context, event json.RawMessage) (interface{}, error) {
	if app.webhookAddress == "" {
		logx.Printf(ctx, "[notice] webhook_address is empty, try fill with lambda function url")
		lc, ok := lambdacontext.FromContext(ctx)
		if !ok {
			logx.Println(ctx, "[error] can not get lambda context")
			return nil, errors.New("can not get lambda context")
		}
		arnObj, err := arn.Parse(lc.InvokedFunctionArn)
		if err != nil {
			logx.Println(ctx, "[error] failed parse InvokedFunctionArn ", err)
			return nil, err
		}
		parts := strings.SplitAfterN(arnObj.Resource, ":", 2)
		var qualifier *string
		if len(parts) >= 2 {
			qualifier = aws.String(parts[1])
		}
		output, err := app.lambdaClient.GetFunctionUrlConfig(ctx, &lambdaapi.GetFunctionUrlConfigInput{
			FunctionName: aws.String(parts[0]),
			Qualifier:    qualifier,
		})
		if err != nil {
			logx.Println(ctx, "[error] failed GetFunctionUrlConfig: ", err)
			return nil, err
		}
		if output.FunctionUrl == nil {
			logx.Println(ctx, "[error] lambda function url is empty")
			return nil, errors.New("lambda function url is empty")
		}
		app.webhookAddress = *output.FunctionUrl
	}
	if err := app.maintenanceChannels(ctx); err != nil {
		logx.Println(ctx, "[error] failed maintenance channels: ", err)
		return nil, err
	}

	return map[string]interface{}{
		"Status": 200,
	}, nil
}
