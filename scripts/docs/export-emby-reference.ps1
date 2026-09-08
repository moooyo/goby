#requires -Version 7.0
<#
.SYNOPSIS
Exports an offline reference from the pinned official Emby Swagger snapshot.
.DESCRIPTION
This script generates documentation. It does not validate the upstream schema,
test an implementation, contact a server, or establish client compatibility.
#>
[CmdletBinding()]
param(
    [string]$RepositoryRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
)

$ErrorActionPreference = 'Stop'
$snapshotRelativePath = 'docs/sources/emby-sdk-openapi.snapshot.json'
$snapshotPath = Join-Path $RepositoryRoot $snapshotRelativePath
$outputDirectory = Join-Path $RepositoryRoot 'docs/api'
$serviceDirectory = Join-Path $outputDirectory 'services'
$null = New-Item -ItemType Directory -Path $outputDirectory -Force
$null = New-Item -ItemType Directory -Path $serviceDirectory -Force
$reference = Get-Content -LiteralPath $snapshotPath -Raw | ConvertFrom-Json -AsHashtable
$httpMethods = @('get', 'post', 'put', 'delete', 'patch', 'head', 'options', 'trace')
$revision = 'bdd0dd7c0801f6e069dff2795d80cddae6f91791'
$revisionUrl = 'https://github.com/MediaBrowser/Emby.SDK/tree/' + $revision
$sourceRepositoryPath = 'Documentation/Download/openapi_v2_noversion.json'
$sourceRawUrl = 'https://raw.githubusercontent.com/MediaBrowser/Emby.SDK/' + $revision + '/' + $sourceRepositoryPath
$sourceRelease = '4.9.5.0'
$sourceDate = '2026-09-09'

function ConvertTo-PointerToken([string]$Value) {
    return $Value.Replace('~', '~0').Replace('/', '~1')
}

function ConvertTo-MarkdownCell($Value) {
    if ($null -eq $Value) { return 'not declared' }
    return ([string]$Value).Replace('|', '&#124;').Replace("`r", '').Replace("`n", '<br>')
}

function ConvertTo-CompactJson($Value) {
    return ConvertTo-Json -InputObject $Value -Depth 100 -Compress
}

function Get-ModelAnchor([string]$Name) {
    return 'model-' + $Name.ToLowerInvariant().Replace('.', '-').Replace('_', '_')
}

function Get-OperationAnchor([string]$OperationId) {
    return 'operation-' + $OperationId.ToLowerInvariant()
}

function Get-SchemaRefs($Node) {
    $found = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
    function Visit-ReferenceNode($Current) {
        if ($Current -is [System.Collections.IDictionary]) {
            if ($Current.Contains('$ref')) { $null = $found.Add([string]$Current['$ref']) }
            foreach ($value in $Current.Values) { Visit-ReferenceNode $value }
        }
        elseif (($Current -is [System.Collections.IEnumerable]) -and ($Current -isnot [string])) {
            foreach ($value in $Current) { Visit-ReferenceNode $value }
        }
    }
    Visit-ReferenceNode $Node
    return @($found | Sort-Object)
}

function Format-Reference([string]$Ref, [string]$ModelPrefix, [string]$SnapshotLink) {
    if ($Ref.StartsWith('#/definitions/')) {
        $modelName = $Ref.Substring('#/definitions/'.Length).Replace('~1', '/').Replace('~0', '~')
        return ('[{0}]({1}#{2})' -f $modelName, $ModelPrefix, (Get-ModelAnchor $modelName))
    }
    return ('[{0}]({1})' -f $Ref, $SnapshotLink)
}

function Format-Schema($Schema, [string]$ModelPrefix, [string]$SnapshotLink) {
    if ($null -eq $Schema) { return 'not declared' }
    if ($Schema -is [bool]) { return ([string]$Schema).ToLowerInvariant() }
    if ($Schema.Contains('$ref')) {
        return Format-Reference $Schema['$ref'] $ModelPrefix $SnapshotLink
    }
    if ($Schema.Contains('schema')) { return Format-Schema $Schema.schema $ModelPrefix $SnapshotLink }
    foreach ($composition in @('allOf', 'oneOf', 'anyOf')) {
        if ($Schema.Contains($composition)) {
            $parts = @($Schema[$composition] | ForEach-Object { Format-Schema $_ $ModelPrefix $SnapshotLink })
            return $composition + '(' + ($parts -join ', ') + ')'
        }
    }
    if ($Schema.type -eq 'array') {
        return 'array&lt;' + (Format-Schema $Schema.items $ModelPrefix $SnapshotLink) + '&gt;'
    }
    if ($Schema.type -eq 'object' -and $Schema.Contains('additionalProperties')) {
        return 'map&lt;string, ' + (Format-Schema $Schema.additionalProperties $ModelPrefix $SnapshotLink) + '&gt;'
    }
    $typeName = if ($Schema.Contains('type')) { [string]$Schema.type } else { 'unspecified' }
    if ($Schema.Contains('format')) { $typeName += ' (' + [string]$Schema.format + ')' }
    if ($Schema.Contains('properties')) { $typeName += ' (inline fields; see source pointer)' }
    return $typeName
}

function Get-Constraints($Schema) {
    $result = [ordered]@{}
    if ($null -eq $Schema -or $Schema -is [bool]) { return $result }
    foreach ($key in @(
        'collectionFormat', 'allowEmptyValue', 'minimum', 'maximum',
        'exclusiveMinimum', 'exclusiveMaximum', 'minLength', 'maxLength',
        'pattern', 'minItems', 'maxItems', 'uniqueItems', 'multipleOf',
        'minProperties', 'maxProperties', 'readOnly', 'nullable', 'x-nullable',
        'discriminator', 'x-deprecated', 'deprecated'
    )) {
        if ($Schema.Contains($key)) { $result[$key] = $Schema[$key] }
    }
    return $result
}

function Format-DefaultEnum($Schema) {
    if ($null -eq $Schema -or $Schema -is [bool]) { return 'not declared' }
    $values = [System.Collections.Generic.List[string]]::new()
    if ($Schema.Contains('default')) {
        $values.Add('default: `' + (ConvertTo-CompactJson $Schema.default) + '`')
    }
    if ($Schema.Contains('enum')) {
        if (@($Schema.enum).Count -gt 8) { $values.Add('enum: see values below') }
        else {
            $enumValues = @($Schema.enum | ForEach-Object { '`' + (ConvertTo-CompactJson $_) + '`' })
            $values.Add('enum: ' + ($enumValues -join ', '))
        }
    }
    if ($Schema.Contains('items') -and $Schema.items -is [System.Collections.IDictionary] -and $Schema.items.Contains('enum')) {
        if (@($Schema.items.enum).Count -gt 8) { $values.Add('item enum: see values below') }
        else {
            $itemValues = @($Schema.items.enum | ForEach-Object { '`' + (ConvertTo-CompactJson $_) + '`' })
            $values.Add('item enum: ' + ($itemValues -join ', '))
        }
    }
    if ($values.Count -eq 0) { return 'not declared' }
    return ConvertTo-MarkdownCell ($values -join '<br>')
}

function Add-LongEnumValues($Lines, $Schema, [string]$Label) {
    if ($null -eq $Schema -or $Schema -is [bool]) { return }
    foreach ($entry in @(
        @{ values = $Schema.enum; label = $Label + ' enum values' },
        @{ values = $Schema.items.enum; label = $Label + ' array-item enum values' }
    )) {
        if (@($entry.values).Count -le 8) { continue }
        $Lines.Add('')
        $Lines.Add('**' + $entry.label + '**')
        $Lines.Add('')
        foreach ($value in $entry.values) {
            $Lines.Add('- `' + (ConvertTo-CompactJson $value) + '`')
        }
    }
}

function Get-ServiceScope([string]$Service) {
    if ($Service -in @('ConnectService', 'PackageService', 'WebAppService')) { return 'OUT-OF-SCOPE' }
    if ($Service -in @(
        'ChannelService', 'DlnaServerService', 'DlnaService', 'GameGenresService',
        'LiveStreamService', 'LiveTvService', 'PartyService', 'SyncService'
    )) { return 'DEFERRED' }
    if ($Service -in @(
        'BackupApi', 'BifService', 'BrandingService', 'CodecParameterService',
        'FeatureService', 'GenericUIApiService', 'InstantMixService',
        'NotificationsService', 'PluginService', 'SuggestionsService',
        'ToneMapOptionsService', 'TrailersService', 'UserNotificationsService'
    )) { return 'EXPANSION' }
    return 'CORE-CANDIDATE'
}

function Get-ServicePurpose([string]$Service) {
    $purposes = @{
        ActivityLogService = 'Administrative activity history.'
        ArtistsService = 'Artist browsing and artist metadata.'
        AudioService = 'Audio delivery routes.'
        BackupApi = 'Backup metadata and restoration; plugin/version provenance remains unresolved.'
        BifService = 'Video preview image index delivery.'
        BrandingService = 'Server presentation settings.'
        ChannelService = 'Available channel listing.'
        CodecParameterService = 'Reading and updating encoding codec parameters.'
        CollectionService = 'Collection membership and creation.'
        ConfigurationService = 'Server configuration and named settings.'
        ConnectService = 'Emby Connect account integration.'
        ContentService = 'User home sections and section item queries.'
        DeviceService = 'Client device records, options, and camera uploads.'
        DisplayPreferencesService = 'Per-client display preferences needed by external clients.'
        DlnaServerService = 'DLNA descriptions, icons, service descriptors, and control routes.'
        DlnaService = 'DLNA profiles and profile management.'
        DynamicHlsService = 'Adaptive audio/video HLS playlists and segments.'
        EncodingInfoService = 'Codec configuration defaults, video codec information, and tone mapping options.'
        EnvironmentService = 'Administrative filesystem discovery.'
        FeatureService = 'Feature availability reports.'
        FfmpegOptionsService = 'Reading and updating FFmpeg options.'
        GameGenresService = 'Game-specific genre browsing.'
        GenericUIApiService = 'Emby generic extension UI contracts.'
        GenresService = 'Genre browsing and genre metadata.'
        HlsSegmentService = 'Stopping active encodings through DELETE and POST aliases.'
        ImageService = 'Item/user image retrieval and image administration.'
        InstantMixService = 'Generated music mixes and audiobook next-up queries.'
        ItemLookupService = 'External metadata search and identification.'
        ItemRefreshService = 'Item metadata refresh requests.'
        ItemsService = 'Filtered item queries and resume listings.'
        ItemUpdateService = 'Item metadata editing.'
        LibraryService = 'Library queries, file downloads, metadata, deletion, and refresh control.'
        LibraryStructureService = 'Library roots, media paths, and library options.'
        LiveStreamService = 'Live TV recording and live-stream file delivery over HTTP and HLS.'
        LiveTvService = 'Tuners, guide data, channels, recordings, and recording schedules.'
        LocalizationService = 'Languages, countries, and localized option metadata.'
        MediaInfoService = 'Playback negotiation and media-source lifecycle.'
        MoviesService = 'Movie recommendations.'
        MusicGenresService = 'Music genre browsing.'
        NotificationsService = 'Administrative notifications and notification type discovery.'
        OfficialRatingService = 'Content rating lookup.'
        OpenApiService = 'API description delivery.'
        PackageService = 'Emby package catalog and installation.'
        PartyService = 'Synchronized group playback.'
        PersonsService = 'People browsing and person metadata.'
        PlaylistService = 'Playlist contents and ordering.'
        PlaystateService = 'Playback reporting and user play-state changes.'
        PluginService = 'Emby plugin lifecycle and configuration.'
        RemoteImageService = 'External image search and image selection.'
        ScheduledTaskService = 'Background task status, execution, and scheduling.'
        SessionsService = 'API keys, authentication providers, active sessions, capabilities, and remote commands.'
        StudiosService = 'Studio browsing and studio metadata.'
        SubtitleOptionsService = 'Reading and updating subtitle options.'
        SubtitleService = 'Subtitle search, provider downloads, delivery, deletion, and media attachments.'
        SuggestionsService = 'Suggested item browsing.'
        SyncService = 'Offline synchronization jobs and targets.'
        SystemService = 'Server discovery, health information, logs, and lifecycle control.'
        TagService = 'Browse facets for tags, codecs, containers, item types, languages, years, and prefixes; tag changes.'
        ToneMapOptionsService = 'Reading and updating full and public tone mapping options.'
        TrailersService = 'Trailer browsing.'
        TvShowsService = 'Season, episode, next-up, missing, and upcoming queries.'
        UniversalAudioService = 'Client-directed audio delivery negotiation.'
        UserLibraryService = 'User item data, browsing, ratings, favorites, shared-item access, and additional video parts.'
        UserNotificationsService = 'Notification service defaults and delivery test requests.'
        UserService = 'User authentication, accounts, configuration, and policy.'
        UserViewsService = 'User library views and grouping.'
        VideoHlsService = 'Audio and video segment retrieval through legacy HLS routes.'
        VideoService = 'Progressive video delivery.'
        VideosService = 'Merging video versions and removing alternate sources.'
        WebAppService = 'Emby web-application configuration pages and localization strings.'
    }
    if ($purposes.ContainsKey($Service)) { return $purposes[$Service] }
    return 'See the pinned source for the service contract.'
}

function Get-ParameterRecord($Parameter, [string]$Pointer) {
    $schema = if ($Parameter.Contains('schema')) { $Parameter.schema } else { $Parameter }
    return [ordered]@{
        name = $Parameter.name
        location = $Parameter.in
        sourceDescription = $Parameter.description
        sourcePointer = $Pointer
        reference = $Parameter['$ref']
        requiredDeclared = if ($Parameter.Contains('required')) { $Parameter.required } else { $null }
        requiredEffectiveBySwagger = if ($Parameter.Contains('required')) { $Parameter.required } else { $false }
        type = $Parameter.type
        format = $Parameter.format
        schema = if ($Parameter.Contains('schema')) { $Parameter.schema } else { $null }
        items = $Parameter.items
        defaultDeclared = $Parameter.Contains('default')
        default = $Parameter.default
        enum = $Parameter.enum
        constraints = Get-Constraints $Parameter
        schemaRefs = @(Get-SchemaRefs $schema)
    }
}

$operations = [System.Collections.Generic.List[object]]::new()
foreach ($path in @($reference.paths.Keys | Sort-Object)) {
    $pathItem = $reference.paths[$path]
    $pathPointer = '#/paths/' + (ConvertTo-PointerToken $path)
    foreach ($method in $httpMethods) {
        if (-not $pathItem.Contains($method)) { continue }
        $operation = $pathItem[$method]
        $pointer = $pathPointer + '/' + $method
        $tags = @($operation.tags)
        $service = if ($tags.Count -gt 0) { $tags[0] } else { 'Untagged' }
        $parameterEntries = [System.Collections.Generic.List[object]]::new()
        $pathParameters = @($pathItem.parameters | Where-Object { $null -ne $_ })
        $operationParameters = @($operation.parameters | Where-Object { $null -ne $_ })
        for ($index = 0; $index -lt $pathParameters.Count; $index++) {
            $parameterEntries.Add([ordered]@{
                raw = $pathParameters[$index]
                pointer = $pathPointer + '/parameters/' + $index
            })
        }
        for ($index = 0; $index -lt $operationParameters.Count; $index++) {
            $parameter = $operationParameters[$index]
            for ($existing = $parameterEntries.Count - 1; $existing -ge 0; $existing--) {
                if ($parameterEntries[$existing].raw.name -eq $parameter.name -and $parameterEntries[$existing].raw.in -eq $parameter.in) {
                    $parameterEntries.RemoveAt($existing)
                }
            }
            $parameterEntries.Add([ordered]@{ raw = $parameter; pointer = $pointer + '/parameters/' + $index })
        }
        $parameterRecords = @($parameterEntries | ForEach-Object { Get-ParameterRecord $_.raw $_.pointer })
        $bodyParameters = @($parameterEntries | Where-Object { $_.raw.in -eq 'body' })
        $responses = [System.Collections.Generic.List[object]]::new()
        foreach ($status in @($operation.responses.Keys | Sort-Object)) {
            $response = $operation.responses[$status]
            $responses.Add([ordered]@{
                status = [string]$status
                sourcePointer = $pointer + '/responses/' + (ConvertTo-PointerToken $status)
                reference = $response['$ref']
                sourceDescription = $response.description
                schema = $response.schema
                headers = $response.headers
                schemaRefs = @(Get-SchemaRefs $response)
            })
        }
        $authParameters = @($parameterEntries | Where-Object {
            $_.raw.name -match '(?i)^(X-Emby-Authorization|Authorization|X-Emby-Token|api_key)$'
        } | ForEach-Object { $_.raw })
        $consumes = if ($operation.Contains('consumes')) { $operation.consumes } else { $reference.consumes }
        $produces = if ($operation.Contains('produces')) { $operation.produces } else { $reference.produces }
        $record = [ordered]@{
            method = $method.ToUpperInvariant()
            path = $path
            operationId = $operation.operationId
            service = $service
            tags = $tags
            sourcePointer = $pointer
            sourceSummary = $operation.summary
            sourceExternalDocs = $operation.externalDocs
            derivedOfficialReferenceUrl = 'https://dev.emby.media/reference/RestAPI/' + $service + '/' + $operation.operationId + '.html'
            derivedOfficialReferencePageChecked = $false
            sourceAuthentication = [ordered]@{
                securityDeclared = $operation.Contains('security')
                security = $operation.security
                'x-RequiredAuthentication' = $operation['x-RequiredAuthentication']
                authorizationParameters = $authParameters
                trust = 'Source declaration only; inconsistent and incomplete; not the project authorization policy.'
            }
            consumes = @($consumes | Where-Object { $null -ne $_ })
            produces = @($produces | Where-Object { $null -ne $_ })
            parameters = $parameterRecords
            requestBody = @($bodyParameters | ForEach-Object {
                [ordered]@{
                    name = $_.raw.name
                    requiredDeclared = $_.raw.required
                    sourcePointer = $_.pointer
                    schema = $_.raw.schema
                    schemaRefs = @(Get-SchemaRefs $_.raw.schema)
                }
            })
            responses = $responses.ToArray()
            requestSchemaRefs = @(Get-SchemaRefs @($parameterEntries | ForEach-Object { $_.raw }))
            responseSchemaRefs = @(Get-SchemaRefs $operation.responses)
            scopeCandidate = Get-ServiceScope $service
            scopeLevel = 'service-level candidate; operation-level P0/P1 is maintained in implementation-scope.md'
            implementationStatus = 'planned-unimplemented'
        }
        $operations.Add($record)
    }
}

$serviceNames = @($operations | ForEach-Object { $_.service } | Sort-Object -Unique)
$declaredTagsWithoutOperations = @($reference.tags | Where-Object { $_.name -notin $serviceNames } | ForEach-Object { $_.name })
$sourceMetadata = [ordered]@{
    repository = 'https://github.com/MediaBrowser/Emby.SDK'
    commit = $revision
    commitUrl = $revisionUrl
    repositoryPath = $sourceRepositoryPath
    rawUrl = $sourceRawUrl
    repositoryReleaseLabel = $sourceRelease
    releaseEvidencePath = 'SampleCode/RestApi/Version.txt'
    retrievedDate = $sourceDate
    snapshot = $snapshotRelativePath
    swagger = $reference.swagger
    sourceInfo = $reference.info
    sourceHost = $reference.host
    sourceBasePath = $reference.basePath
    sourceOriginalBasePath = $reference['x-original-basePath']
    sourceSchemes = $reference.schemes
    globalSecurityDeclared = $reference.Contains('security')
    globalSecurity = $reference.security
    securityDefinitions = $reference.securityDefinitions
    releaseInterpretation = 'The release label comes from repository evidence; info.version is absent from the snapshot.'
    parsingNote = 'The raw snapshot retains a trailing comma in info. PowerShell 7 ConvertFrom-Json accepts it; generated JSON is reserialized without that comma. The source file is not changed.'
}
$inventory = [ordered]@{
    formatVersion = 1
    purpose = 'Offline source-derived Emby HTTP reference; not an implementation or compatibility claim.'
    source = $sourceMetadata
    counts = [ordered]@{
        paths = $reference.paths.Count
        operations = $operations.Count
        servicesWithOperations = $serviceNames.Count
        declaredTags = @($reference.tags).Count
        definitions = $reference.definitions.Count
    }
    declaredTagsWithoutOperations = $declaredTagsWithoutOperations
    scopeLegend = [ordered]@{
        'CORE-CANDIDATE' = 'Relevant to the initial compatibility and administration surface; not every operation is P0 or P1.'
        EXPANSION = 'Useful after the initial client and administration workflows.'
        DEFERRED = 'A distinct subsystem outside the initial delivery scope.'
        'OUT-OF-SCOPE' = 'Excluded from the current project product scope.'
    }
    implementationScopeDocument = 'docs/api/implementation-scope.md'
    operations = $operations.ToArray()
}
$inventory | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath (Join-Path $outputDirectory 'inventory.json') -Encoding utf8

$catalog = [System.Collections.Generic.List[string]]::new()
$catalog.Add('# Emby HTTP API reference catalog')
$catalog.Add('')
$catalog.Add('This catalog inventories the pinned official API description for a Linux-only Go server')
$catalog.Add('with a React/MUI administrator dashboard. Playback remains available to external Emby-compatible')
$catalog.Add('clients; this project does not provide an end-user web playback application.')
$catalog.Add('')
$catalog.Add('All entries are source-derived; `planned-unimplemented` records the original research baseline.')
$catalog.Add('Current code and test evidence are tracked in the [implemented surface](implemented.md).')
$catalog.Add('Listing a route does not establish implementation or real-client compatibility.')
$catalog.Add('')
$catalog.Add('## Source and offline navigation')
$catalog.Add('')
$catalog.Add('- [Official REST API guide](https://dev.emby.media/doc/restapi/index.html).')
$catalog.Add('- [Pinned official SDK commit](' + $revisionUrl + '), repository release label **' + $sourceRelease + '**.')
$catalog.Add('- [Original source file](' + $sourceRawUrl + ') and [source provenance notes](../sources/README.md).')
$catalog.Add('- [Complete Swagger 2.0 snapshot](../sources/emby-sdk-openapi.snapshot.json).')
$catalog.Add('- [Machine-readable operation inventory](inventory.json).')
$catalog.Add('- [All DTOs and enums](models.md).')
$catalog.Add('- [Operation-level implementation scope](implementation-scope.md).')
$catalog.Add('- [Older static documentation snapshot](../sources/emby-openapi.snapshot.json), used only for historical comparison.')
$catalog.Add('')
$catalog.Add(('The pinned source contains **{0} paths**, **{1} HTTP operations**, **{2} service groups with operations**,' -f $reference.paths.Count, $operations.Count, $serviceNames.Count))
$catalog.Add(('and **{0} definitions**. It declares {1} tags.' -f $reference.definitions.Count, @($reference.tags).Count))
if ($declaredTagsWithoutOperations.Count -gt 0) {
    $catalog.Add('Declared tags without operations: `' + ($declaredTagsWithoutOperations -join '`, `') + '`.')
}
else { $catalog.Add('Declared tags without operations: none.') }
$catalog.Add('The repository release label is provenance, not an inferred `info.version`: the source omits')
$catalog.Add('both `info.title` and `info.version`. The source host `emby.restapi` and its HTTP scheme are')
$catalog.Add('documentation values, not deployment recommendations. The source does not declare `basePath`;')
$catalog.Add('it records `/emby` in the vendor extension `x-original-basePath`. The compatibility transport layer')
$catalog.Add('must handle client base-URL conventions explicitly. The raw file retains a trailing comma in `info`;')
$catalog.Add('PowerShell 7 accepts that syntax for this documentation export. The inventory is reserialized as JSON,')
$catalog.Add('while the original snapshot remains unchanged. No schema validator has been run.')
$catalog.Add('')
$catalog.Add('### Reading source declarations safely')
$catalog.Add('')
$catalog.Add('- The original `security`, `x-RequiredAuthentication`, and authorization header definitions are preserved.')
$catalog.Add('  They are evidence to investigate, not the project permission policy. For example, the source labels')
$catalog.Add('  `POST /Users/AuthenticateByName` as requiring user authentication.')
$catalog.Add('- `securityDefinitions.embyauth` is empty. Do not feed this snapshot directly into an authorization')
$catalog.Add('  generator and assume its result is safe or complete.')
$catalog.Add('- Parameter tables distinguish explicit `required` from an omitted declaration. DTO fields without')
$catalog.Add('  a `required` entry are not automatically optional in every real request or response.')
$catalog.Add('- The source contains no object-level `required` arrays or schema `default` declarations. Defaults')
$catalog.Add('  stated in prose are retained as source descriptions in the inventory, not promoted into schema defaults.')
$catalog.Add('- Some parameter types are unspecified. Structural serialization fields can disagree with prose:')
$catalog.Add('  `POST /Sessions/{Id}/Playing` describes `ItemIds` as comma delimited but declares `collectionFormat: multi`.')
$catalog.Add('  The inventory preserves both statements; client fixtures must resolve that discrepancy.')
$catalog.Add('- A missing response schema does not prove an empty body. Binary delivery, HTTP Range behavior,')
$catalog.Add('  redirects, caching, subtitle formats, HLS semantics, and WebSocket events require separate contracts.')
$catalog.Add('- Every operation has an exact JSON Pointer into the local snapshot. Use that snapshot for complete')
$catalog.Add('  nested schemas, source prose, vendor extensions, and reusable response declarations.')
$catalog.Add('- Per-operation official reference links are derived from the documented URL pattern. Their pages')
$catalog.Add('  were not individually checked; the pinned local source is the parameter evidence.')
$catalog.Add('')
$catalog.Add('## Service-level scope candidates')
$catalog.Add('')
$catalog.Add('`CORE-CANDIDATE` means the service contains relevant initial workflows. It does **not** place every')
$catalog.Add('operation in that service into the MVP. P0/P1 selections and exclusions live in')
$catalog.Add('[implementation-scope.md](implementation-scope.md). `EXPANSION` follows the initial workflows;')
$catalog.Add('`DEFERRED` covers separate subsystems; `OUT-OF-SCOPE` is excluded from this product scope.')
$catalog.Add('')
$catalog.Add('| Service | Operations | Candidate scope | Purpose |')
$catalog.Add('| --- | ---: | --- | --- |')
foreach ($service in $serviceNames) {
    $serviceOperations = @($operations | Where-Object { $_.service -eq $service })
    $catalog.Add(('| [{0}](services/{0}.md) | {1} | {2} | {3} |' -f $service, $serviceOperations.Count, (Get-ServiceScope $service), (Get-ServicePurpose $service)))
}
$catalog.Add('')
$catalog.Add('## Complete operation index')
$catalog.Add('')
$catalog.Add('Every entry below links to parameters, request/response schemas, source authentication declarations,')
$catalog.Add('and its source pointer. All entries have status `planned-unimplemented`. Scope is inherited from')
$catalog.Add('the service as a candidate classification, not an operation delivery commitment.')
foreach ($service in $serviceNames) {
    $catalog.Add('')
    $catalog.Add('### ' + $service)
    $catalog.Add('')
    $catalog.Add('Candidate: `' + (Get-ServiceScope $service) + '`. ' + (Get-ServicePurpose $service))
    $catalog.Add('')
    $catalog.Add('| Method | Path | Operation ID and local reference |')
    $catalog.Add('| --- | --- | --- |')
    foreach ($operation in @($operations | Where-Object { $_.service -eq $service })) {
        $catalog.Add(('| `{0}` | `{1}` | [{2}](services/{3}.md#{4}) |' -f $operation.method, $operation.path, $operation.operationId, $service, (Get-OperationAnchor $operation.operationId)))
    }
}
$catalog.Add('')
$catalog.Add('## Regeneration')
$catalog.Add('')
$catalog.Add('The exporter requires PowerShell 7 and reads only the pinned local snapshot. It writes this catalog,')
$catalog.Add('`inventory.json`, `models.md`, and one file per service. It does not fetch a newer release or perform')
$catalog.Add('tests, schema validation, build verification, runtime probes, or compatibility verification.')
$catalog.Add('')
$catalog.Add('```powershell')
$catalog.Add('./scripts/docs/export-emby-reference.ps1')
$catalog.Add('```')
$catalog.Add('')
$catalog.Add('Generated files are a documentation projection. Hand-maintained scope decisions belong in')
$catalog.Add('`implementation-scope.md` so regeneration cannot silently promote routes into the MVP.')
$catalog -join "`n" | Set-Content -LiteralPath (Join-Path $outputDirectory 'catalog.md') -Encoding utf8

foreach ($service in $serviceNames) {
    $serviceOperations = @($operations | Where-Object { $_.service -eq $service })
    $lines = [System.Collections.Generic.List[string]]::new()
    $lines.Add('# ' + $service)
    $lines.Add('')
    $lines.Add((Get-ServicePurpose $service))
    $lines.Add('')
    $lines.Add(('**{0} HTTP operations**. Service candidate: `{1}`. Status: `planned-unimplemented`.' -f $serviceOperations.Count, (Get-ServiceScope $service)))
    $lines.Add('This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).')
    $lines.Add('')
    $lines.Add('[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)')
    $lines.Add('')
    $lines.Add('The source authentication text is preserved verbatim below and may be inconsistent.')
    $lines.Add('It is not the project authorization policy. Required/default/enum columns report source declarations.')
    $lines.Add('Missing schema declarations are gaps to investigate, not proof that a value or body is absent.')
    $lines.Add('Official reference URLs are derived links; these individual pages have not been checked.')
    $lines.Add('')
    $lines.Add('## Operations')
    $lines.Add('')
    foreach ($operation in $serviceOperations) {
        $lines.Add(('- [`{0} {1}`](#{2})' -f $operation.method, $operation.path, (Get-OperationAnchor $operation.operationId)))
    }
    foreach ($operation in $serviceOperations) {
        $rawOperation = $reference.paths[$operation.path][$operation.method.ToLowerInvariant()]
        $lines.Add('')
        $lines.Add('<a id="' + (Get-OperationAnchor $operation.operationId) + '"></a>')
        $lines.Add('')
        $lines.Add('## ' + $operation.method + ' ' + $operation.path)
        $lines.Add('')
        $lines.Add('- Operation ID: `' + $operation.operationId + '`.')
        $lines.Add('- Service candidate: `' + $operation.scopeCandidate + '`; status: `planned-unimplemented`.')
        $lines.Add('- Source pointer: `' + $operation.sourcePointer + '`.')
        $lines.Add('- [Derived official reference](' + $operation.derivedOfficialReferenceUrl + ').')
        $consumesText = if ($operation.consumes.Count -gt 0) { '`' + ($operation.consumes -join '`, `') + '`' } else { 'not declared' }
        $producesText = if ($operation.produces.Count -gt 0) { '`' + ($operation.produces -join '`, `') + '`' } else { 'not declared' }
        $lines.Add('- Consumes: ' + $consumesText + '.')
        $lines.Add('- Produces: ' + $producesText + '.')
        $lines.Add('')
        $lines.Add('### Source authentication declarations')
        $lines.Add('')
        $authProjection = [ordered]@{
            securityDeclared = $operation.sourceAuthentication.securityDeclared
            security = $operation.sourceAuthentication.security
            'x-RequiredAuthentication' = $operation.sourceAuthentication['x-RequiredAuthentication']
        }
        $lines.Add('```json')
        $lines.Add(($authProjection | ConvertTo-Json -Depth 20))
        $lines.Add('```')
        $lines.Add('')
        $lines.Add('The source header declarations are included in the parameter table and retained verbatim in')
        $lines.Add('`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.')
        $lines.Add('These source declarations require a separate permission decision before implementation.')
        $lines.Add('')
        $lines.Add('### Parameters')
        $lines.Add('')
        if ($operation.parameters.Count -eq 0) {
            $lines.Add('No parameters are declared in the source for this operation.')
        }
        else {
            $lines.Add('| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |')
            $lines.Add('| --- | --- | --- | --- | --- | --- |')
            foreach ($parameter in $operation.parameters) {
                $rawParameter = $null
                if ($parameter.sourcePointer.StartsWith($operation.sourcePointer + '/parameters/')) {
                    $parameterIndex = [int]($parameter.sourcePointer.Split('/')[-1])
                    $rawParameter = $rawOperation.parameters[$parameterIndex]
                }
                else {
                    $parameterIndex = [int]($parameter.sourcePointer.Split('/')[-1])
                    $rawParameter = $reference.paths[$operation.path].parameters[$parameterIndex]
                }
                $required = if ($null -eq $parameter.requiredDeclared) { 'omitted (Swagger default: false)' } else { ([string]$parameter.requiredDeclared).ToLowerInvariant() }
                $constraints = if ($parameter.constraints.Count -gt 0) { '`' + (ConvertTo-CompactJson $parameter.constraints) + '`' } else { 'not declared' }
                $lines.Add(('| `{0}` | `{1}` | {2} | {3} | {4} | {5} |' -f $parameter.name, $parameter.location, $required, (Format-Schema $rawParameter '../models.md' '../../sources/emby-sdk-openapi.snapshot.json'), (Format-DefaultEnum $rawParameter), (ConvertTo-MarkdownCell $constraints)))
            }
            foreach ($rawParameter in @($reference.paths[$operation.path].parameters) + @($rawOperation.parameters)) {
                if ($null -ne $rawParameter) { Add-LongEnumValues $lines $rawParameter ('`' + $rawParameter.name + '`') }
            }
        }
        $lines.Add('')
        $lines.Add('### Request body and model references')
        $lines.Add('')
        if ($operation.requestBody.Count -eq 0) {
            $lines.Add('No `in: body` parameter is declared. Query, header, or form parameters may still carry input.')
        }
        else {
            foreach ($body in $operation.requestBody) {
                $lines.Add('- `' + $body.name + '`: ' + (Format-Schema $body.schema '../models.md' '../../sources/emby-sdk-openapi.snapshot.json') + '.')
                $lines.Add('  Source pointer: `' + $body.sourcePointer + '`.')
            }
        }
        if ($operation.requestSchemaRefs.Count -gt 0) {
            $lines.Add('')
            $lines.Add('Direct schema references in all request parameters:')
            $lines.Add('')
            foreach ($ref in $operation.requestSchemaRefs) {
                $lines.Add('- ' + (Format-Reference $ref '../models.md' '../../sources/emby-sdk-openapi.snapshot.json') + '.')
            }
        }
        $lines.Add('')
        $lines.Add('### Responses')
        $lines.Add('')
        $lines.Add('| Status | Schema or reusable response reference | Source description | Source pointer |')
        $lines.Add('| --- | --- | --- | --- |')
        foreach ($response in $operation.responses) {
            $responseType = if ($response.reference) {
                Format-Reference $response.reference '../models.md' '../../sources/emby-sdk-openapi.snapshot.json'
            }
            else { Format-Schema $response.schema '../models.md' '../../sources/emby-sdk-openapi.snapshot.json' }
            $lines.Add(('| `{0}` | {1} | {2} | `{3}` |' -f $response.status, $responseType, (ConvertTo-MarkdownCell $response.sourceDescription), $response.sourcePointer))
        }
        if (@($operation.responses | Where-Object { $null -ne $_.headers }).Count -gt 0) {
            $lines.Add('')
            $lines.Add('Response header declarations are retained in `inventory.json` and the source snapshot.')
        }
        $lines.Add('')
        $lines.Add('Reusable error responses are defined under `#/responses` in the source; their error body schemas')
        $lines.Add('are not declared. A listed HTTP status is a source claim, not observed server behavior.')
    }
    $lines -join "`n" | Set-Content -LiteralPath (Join-Path $serviceDirectory ($service + '.md')) -Encoding utf8
}

$models = [System.Collections.Generic.List[string]]::new()
$models.Add('# Emby data model reference')
$models.Add('')
$models.Add(('This offline index covers all **{0} definitions** in the [pinned Swagger snapshot](../sources/emby-sdk-openapi.snapshot.json).' -f $reference.definitions.Count))
$models.Add('It lists source field names, types, required declarations, defaults, enums, and constraints without')
$models.Add('duplicating repetitive source descriptions. The complete nested schema and vendor extensions remain')
$models.Add('available in that local snapshot at each JSON Pointer. [Return to the API catalog](catalog.md).')
$models.Add('')
$models.Add('## Interpretation and Go implementation notes')
$models.Add('')
$models.Add('- Preserve the exact PascalCase JSON field names and case-sensitive enum strings at the compatibility boundary.')
$models.Add('- A missing object-level `required` array means the source does not require a field; it does not prove that')
$models.Add('  real clients tolerate its omission. Presence, `null`, empty values, and endpoint-specific projections need fixtures.')
$models.Add('- Use Go `int64` for tick values and source `int64` fields. Preserve IDs as source-compatible strings when')
$models.Add('  declared as strings. Do not expose database keys merely because an identifier looks numeric.')
$models.Add('- Date-time format labels and numeric formats are source metadata, not guarantees of exact serialization.')
$models.Add('- Separate wire DTOs from storage entities. A large response model such as `BaseItemDto` is not a database schema.')
$models.Add('- Linked references show the immediate schema graph. Self-references and nested references are intentionally')
$models.Add('  not expanded recursively; use the model links and source pointer to navigate them offline.')
$models.Add('- A model listed here may belong to a deferred or excluded service. Inclusion does not commit the project')
$models.Add('  to implementing that feature, its full model, or every source field.')
$models.Add('')
$models.Add('## Definition index')
$models.Add('')
$models.Add('| Definition | Source type | Declared properties |')
$models.Add('| --- | --- | ---: |')
foreach ($name in @($reference.definitions.Keys | Sort-Object)) {
    $schema = $reference.definitions[$name]
    $propertyCount = if ($schema.Contains('properties')) { $schema.properties.Count } else { 0 }
    $models.Add(('| [{0}](#{1}) | {2} | {3} |' -f $name, (Get-ModelAnchor $name), (ConvertTo-MarkdownCell $schema.type), $propertyCount))
}
foreach ($name in @($reference.definitions.Keys | Sort-Object)) {
    $schema = $reference.definitions[$name]
    $pointer = '#/definitions/' + (ConvertTo-PointerToken $name)
    $models.Add('')
    $models.Add('<a id="' + (Get-ModelAnchor $name) + '"></a>')
    $models.Add('')
    $models.Add('## ' + $name)
    $models.Add('')
    $models.Add('- Source pointer: `' + $pointer + '`.')
    $models.Add('- Type: ' + (Format-Schema $schema 'models.md' '../sources/emby-sdk-openapi.snapshot.json') + '.')
    if ($schema.Contains('x-internal-ref-name')) {
        $models.Add('- Source internal type name: `' + $schema['x-internal-ref-name'] + '`.')
    }
    if ($schema.Contains('required')) {
        $models.Add('- Object-level required declaration: `' + (ConvertTo-CompactJson $schema.required) + '`.')
    }
    else { $models.Add('- Object-level required declaration: omitted.') }
    if ($schema.Contains('enum') -or $schema.Contains('default')) {
        $models.Add('- ' + (Format-DefaultEnum $schema) + '.')
    }
    $topConstraints = Get-Constraints $schema
    if ($topConstraints.Count -gt 0) { $models.Add('- Constraints: `' + (ConvertTo-CompactJson $topConstraints) + '`.') }
    if ($schema.Contains('additionalProperties')) {
        $models.Add('- Additional property values: ' + (Format-Schema $schema.additionalProperties 'models.md' '../sources/emby-sdk-openapi.snapshot.json') + '.')
    }
    Add-LongEnumValues $models $schema ('`' + $name + '`')
    if ($schema.Contains('properties') -and $schema.properties.Count -gt 0) {
        $models.Add('')
        $models.Add('| Field | Type or reference | In required array | Default / enum | Other constraints |')
        $models.Add('| --- | --- | --- | --- | --- |')
        foreach ($field in $schema.properties.Keys) {
            $property = $schema.properties[$field]
            $required = if ($field -in @($schema.required)) { 'yes' } else { 'not listed' }
            $constraints = Get-Constraints $property
            $constraintText = if ($constraints.Count -gt 0) { '`' + (ConvertTo-CompactJson $constraints) + '`' } else { 'not declared' }
            $models.Add(('| `{0}` | {1} | {2} | {3} | {4} |' -f $field, (Format-Schema $property 'models.md' '../sources/emby-sdk-openapi.snapshot.json'), $required, (Format-DefaultEnum $property), (ConvertTo-MarkdownCell $constraintText)))
        }
        foreach ($field in $schema.properties.Keys) {
            Add-LongEnumValues $models $schema.properties[$field] ('`' + $field + '`')
        }
        $models.Add('')
        $models.Add('Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.')
    }
    else {
        $models.Add('')
        $models.Add('No named properties are declared on this definition. Its scalar, enum, array, map, or composition')
        $models.Add('declarations above and the full source schema define its shape.')
    }
    $refs = @(Get-SchemaRefs $schema)
    if ($refs.Count -gt 0) {
        $models.Add('')
        $models.Add('Immediate referenced definitions:')
        $models.Add('')
        foreach ($ref in $refs) { $models.Add('- ' + (Format-Reference $ref 'models.md' '../sources/emby-sdk-openapi.snapshot.json') + '.') }
    }
}
$models -join "`n" | Set-Content -LiteralPath (Join-Path $outputDirectory 'models.md') -Encoding utf8

Write-Output ('Exported documentation for {0} operations, {1} services, and {2} definitions.' -f $operations.Count, $serviceNames.Count, $reference.definitions.Count)
Write-Output 'This generation step performs no tests or compatibility verification.'
