local caller = std.native('caller_identity')();
{
  Description: 'Example of gdnotify',
  Architectures: ['arm64'],
  Environment: {
    Variables: {
      GOOGLE_APPLICATION_CREDENTIALS: 'arn:aws:ssm:ap-northeast-1:%s:parameter/gdnotify/GOOGLE_APPLICATION_CREDENTIALS' % [caller.Account],
      GDNOTIFY_EVENTBRIDGE_EVENT_BUS: 'gdnotify',
      GDNOTIFY_LOG_FORMAT: 'json',
      GDNOTIFY_LOG_LEVEL: 'info',
      GDNOTIFY_EXPIRATION: '168h',
      GDNOTIFY_DDB_AUTO_CREATE: 'true',
      TZ: 'Asia/Tokyo',
    },
  },
  FunctionName: 'gdnotify',
  Handler: 'bootstrap',
  MemorySize: 128,
  Role: 'arn:aws:iam::%s:role/gdnotify' % [caller.Account],
  Runtime: 'provided.al2023',
  Tags: {},
  Timeout: 30,
  TracingConfig: {
    Mode: 'PassThrough',
  },
  LoggingConfig: {
    LogFormat: 'JSON',
  },
}
