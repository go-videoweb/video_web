param([string]$Docker = 'docker')

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$compose = Join-Path $root 'docker-compose.yaml'
$example = Join-Path $root '.env.example'
$bootstrap = Join-Path $PSScriptRoot 'elasticsearch-bootstrap.yaml'
$composeArgs = @('compose', '--project-directory', $root, '--env-file', $example, '-f', $compose)

# Assert-Condition 在静态模型不满足约束时终止检查。
# 输入：Condition 为断言结果；Message 为失败原因。不会操作容器或数据。
function Assert-Condition([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

# Read-Model 解析示例环境下的 Compose JSON，不连接 Docker daemon。
# 输入：IncludeBootstrap 指定是否合并首次建群配置。语法失败时抛错。
# 输出：反序列化的配置对象；包含示例凭据，仅在内存中检查，不打印完整模型。
function Read-Model([switch]$IncludeBootstrap) {
    $arguments = $composeArgs
    if ($IncludeBootstrap) { $arguments += @('-f', $bootstrap) }
    $output = & $Docker @arguments config --format json
    Assert-Condition ($LASTEXITCODE -eq 0) 'Compose model validation failed.'
    return ($output -join "`n" | ConvertFrom-Json)
}

# 隔离调用者环境对插值的影响，检查结束后还原进程环境。
$names = [regex]::Matches((Get-Content -LiteralPath $compose -Raw -Encoding UTF8), '\$\{([A-Z][A-Z0-9_]*)') |
    ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique
$saved = @{}

# 保证模板中每个可配置参数都能在环境示例中找到，避免漏掉覆盖入口。
$exampleNames = [regex]::Matches((Get-Content -LiteralPath $example -Raw -Encoding UTF8), '(?m)^([A-Z][A-Z0-9_]*)=') |
    ForEach-Object { $_.Groups[1].Value }
Assert-Condition (($exampleNames | Sort-Object -Unique).Count -eq $exampleNames.Count) 'Duplicate variable in .env.example.'
foreach ($name in $names) {
    Assert-Condition ($exampleNames -contains $name) "Missing variable in .env.example: $name"
}

foreach ($name in $names) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    if (Test-Path -LiteralPath "Env:$name") { Remove-Item -LiteralPath "Env:$name" }
}

try {
    & $Docker @composeArgs config --quiet
    Assert-Condition ($LASTEXITCODE -eq 0) 'Compose syntax validation failed.'
    $model = Read-Model
    $expected = @('mongodb', 'dtm', 'jaeger-alo', 'rabbitmq', 'es01', 'es02', 'es03', 'kibana')
    $actual = @($model.services.PSObject.Properties.Name)
    Assert-Condition ($actual.Count -eq $expected.Count) 'Unexpected service count.'
    foreach ($name in $expected) {
        Assert-Condition ($actual -contains $name) "Missing service: $name"
    }

    $ports = @{}
    foreach ($property in $model.services.PSObject.Properties) {
        foreach ($port in $property.Value.ports) {
            Assert-Condition ($port.host_ip -eq '127.0.0.1') 'A default port is not loopback-only.'
            $key = "$($port.host_ip):$($port.published)/$($port.protocol)"
            Assert-Condition (-not $ports.ContainsKey($key)) 'Duplicate published port.'
            $ports[$key] = $true
        }
        foreach ($volume in $property.Value.volumes) {
            if ($volume.read_only) {
                Assert-Condition (-not $volume.bind.create_host_path) 'Missing config/cert paths must not be created silently.'
            }
        }
    }

    Assert-Condition ($ports.Count -eq 18) 'Unexpected published port count.'
    Assert-Condition ($model.networks.elastic.external -eq $true) 'ES network must remain external.'
    Assert-Condition ($model.networks.elastic.name -eq 'elastic') 'Unexpected external network name.'
    $ips = @{}
    foreach ($name in @('es01', 'es02', 'es03', 'kibana')) {
        $service = $model.services.$name
        $ip = $service.networks.elastic.ipv4_address
        Assert-Condition ($ip -like '172.18.0.*') 'Unexpected default static IP.'
        Assert-Condition (-not $ips.ContainsKey($ip)) 'Duplicate static IP.'
        $ips[$ip] = $true
        Assert-Condition ($service.image.EndsWith(':8.17.0')) 'ES and Kibana versions must match.'
    }
    foreach ($name in @('es01', 'es02', 'es03')) {
        $service = $model.services.$name
        Assert-Condition ($service.environment.'xpack.security.enabled' -eq 'true') 'ES authentication is disabled.'
        Assert-Condition ($service.environment.'xpack.security.http.ssl.enabled' -eq 'true') 'ES HTTP TLS is disabled.'
        Assert-Condition ($service.environment.'xpack.security.transport.ssl.enabled' -eq 'true') 'ES transport TLS is disabled.'
        Assert-Condition ($service.environment.'xpack.security.transport.ssl.verification_mode' -eq 'full') 'ES transport hostname checks are disabled.'
        Assert-Condition (-not ($service.environment.PSObject.Properties.Name -contains 'cluster.initial_master_nodes')) 'Bootstrap setting leaked into routine startup.'
        Assert-Condition ($service.healthcheck.test[1].Contains('$${ELASTIC_PASSWORD}')) 'Healthcheck password must be deferred to the container shell.'
    }
    Assert-Condition ($model.services.rabbitmq.environment.RABBITMQ_DEFAULT_VHOST -eq 'video_web') 'RabbitMQ vhost variable is incorrect.'
    Assert-Condition (-not ($model.services.kibana.environment.PSObject.Properties.Name -contains 'ES_JAVA_OPTS')) 'ES heap options must not be placed on Kibana.'
    Assert-Condition ($model.services.kibana.environment.ELASTICSEARCH_USERNAME -eq 'kibana_system') 'Kibana must use its service account.'
    Assert-Condition ($model.services.dtm.working_dir -eq '/var/lib/dtm') 'DTM state path is not persistent.'

    $firstStart = Read-Model -IncludeBootstrap
    foreach ($name in @('es01', 'es02', 'es03')) {
        Assert-Condition ($firstStart.services.$name.environment.'cluster.initial_master_nodes' -eq 'es01,es02,es03') 'First-start override is incomplete.'
    }

    $credentials = @('MONGO_PASSWORD', 'RABBITMQ_PASSWORD', 'ELASTIC_PASSWORD', 'KIBANA_PASSWORD')
    $emptyFile = if ($env:OS -eq 'Windows_NT') { 'NUL' } else { '/dev/null' }
    $missingArgs = @('compose', '--project-directory', $root, '--env-file', $emptyFile, '-f', $compose)
    foreach ($name in $credentials) { Set-Item -LiteralPath "Env:$name" -Value 'CHECK_ONLY' }
    & $Docker @missingArgs config --quiet
    Assert-Condition ($LASTEXITCODE -eq 0) 'Credential test baseline failed.'
    foreach ($name in $credentials) {
        Remove-Item -LiteralPath "Env:$name"
        $ErrorActionPreference = 'Continue'
        try {
            $failure = & $Docker @missingArgs config --quiet 2>&1
            $exitCode = $LASTEXITCODE
        }
        finally { $ErrorActionPreference = 'Stop' }
        Assert-Condition ($exitCode -ne 0) "A missing required credential was accepted: $name"
        Assert-Condition (($failure -join "`n").Contains("required variable $name")) "Credential test failed for an unrelated reason: $name"
        Set-Item -LiteralPath "Env:$name" -Value 'CHECK_ONLY'
    }
    foreach ($name in $credentials) { Remove-Item -LiteralPath "Env:$name" }

    $env:MONGO_ROOT = 'D:/Compose path with spaces/mongodb'
    $env:ES02_HTTP_PORT = '19202'
    $env:ELASTIC_VERSION = '8.17.1'
    $env:ELASTIC_NETWORK_NAME = 'videoweb-elastic-test'
    $overridden = Read-Model
    $mongoData = $overridden.services.mongodb.volumes | Where-Object { $_.target -eq '/data/db' }
    Assert-Condition ($mongoData.source.Replace('\', '/').EndsWith('Compose path with spaces/mongodb/data')) 'Windows bind path override failed.'
    Assert-Condition ($overridden.services.es02.ports[0].published -eq '19202') 'ES port override failed.'
    Assert-Condition ($overridden.networks.elastic.name -eq 'videoweb-elastic-test') 'Network name override failed.'
    foreach ($name in @('es01', 'es02', 'es03', 'kibana')) {
        Assert-Condition ($overridden.services.$name.image.EndsWith(':8.17.1')) 'Shared image version override failed.'
    }

    Write-Output "Compose checks passed: 8 services, $($ports.Count) unique loopback ports, TLS/network mounts, bootstrap override, 4 required credentials, path/port/version overrides. No containers started."
}
finally {
    foreach ($name in $names) {
        if ($null -eq $saved[$name]) {
            if (Test-Path -LiteralPath "Env:$name") { Remove-Item -LiteralPath "Env:$name" }
        }
        else {
            Set-Item -LiteralPath "Env:$name" -Value $saved[$name]
        }
    }
}
