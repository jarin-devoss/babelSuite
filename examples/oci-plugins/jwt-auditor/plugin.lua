local json = require("cjson")
local http = require("resty.http")

local function as_array(t) return #t == 0 and json.empty_array or t end

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "jwt-auditor"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- base64url → standard base64 → decode
local function b64url_decode(s)
    s = s:gsub("%-", "+"):gsub("_", "/")
    local pad = #s % 4
    if pad == 2 then s = s .. "=="
    elseif pad == 3 then s = s .. "=" end
    return ngx.decode_base64(s) or ""
end

local function decode_jwt(token)
    local parts = {}
    for p in token:gmatch("[^%.]+") do parts[#parts + 1] = p end
    if #parts ~= 3 then return nil, "not a valid JWT (expected 3 dot-separated parts)" end
    local ok_h, hdr = pcall(json.decode, b64url_decode(parts[1]))
    local ok_p, pld = pcall(json.decode, b64url_decode(parts[2]))
    if not ok_h then return nil, "could not decode JWT header" end
    if not ok_p then return nil, "could not decode JWT payload" end
    return {header = hdr, payload = pld, raw_parts = parts}, nil
end

local function fetch_token(auth_url, username, password, token_field)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(auth_url, {
        method  = "POST",
        body    = json.encode({username = username, password = password}),
        headers = {["Content-Type"] = "application/json"},
    })
    if err then return nil, "auth request failed: " .. err end
    if res.status ~= 200 then return nil, "auth returned HTTP " .. res.status end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "could not parse auth response" end
    local field = token_field or "token"
    local token = data[field] or data["access_token"] or data["jwt"]
    if not token or token == "" then
        return nil, "no token found in response (tried '" .. field .. "', 'access_token', 'jwt')"
    end
    return token, nil
end

local WEAK_ALGS     = {none = true, None = true, NONE = true}
local SYMMETRIC_ALGS = {HS256 = true, HS384 = true, HS512 = true}

local function audit_claims(jwt_data, cfg, findings, severity)
    local hdr = jwt_data.header
    local pld = jwt_data.payload
    local alg = hdr.alg or "unknown"

    -- algorithm check
    if WEAK_ALGS[alg] then
        table.insert(findings, {
            label    = "jwt.alg.none",
            severity = "critical",
            detail   = "algorithm is 'none' — token is unsigned and can be trivially forged",
        })
    elseif SYMMETRIC_ALGS[alg] then
        table.insert(findings, {
            label    = "jwt.alg.symmetric",
            severity = "medium",
            detail   = alg .. " uses a shared secret; prefer RS256 or ES256 for public-facing services",
        })
    end

    -- expiry check
    local exp = tonumber(pld.exp)
    local iat = tonumber(pld.iat)
    if not exp then
        table.insert(findings, {
            label    = "jwt.claims.no_exp",
            severity = severity,
            detail   = "token has no 'exp' claim — it never expires",
        })
    else
        if exp < ngx.now() then
            table.insert(findings, {
                label    = "jwt.claims.expired",
                severity = "low",
                detail   = string.format("token expired at %d (now=%d)", exp, math.floor(ngx.now())),
            })
        end
        if iat then
            local max_hours = tonumber(cfg.max_lifetime_hours) or 24
            local lifetime_h = (exp - iat) / 3600
            if lifetime_h > max_hours then
                table.insert(findings, {
                    label    = "jwt.lifetime.too_long",
                    severity = severity,
                    detail   = string.format("token lifetime %.1fh exceeds max %.0fh", lifetime_h, max_hours),
                })
            end
        end
    end

    -- required claims
    for _, claim in ipairs(cfg.required_claims or {"sub", "iss", "exp", "iat"}) do
        if pld[claim] == nil then
            table.insert(findings, {
                label    = "jwt.claims.missing",
                severity = severity,
                detail   = "required claim '" .. claim .. "' is absent from the token payload",
            })
        end
    end
end

-- check_token: fetch a JWT from an auth endpoint and audit its structure and claims
local function run_check_token(cfg)
    local findings = {}
    local severity = cfg.severity or "high"

    local token, err = fetch_token(cfg.auth_url or "", cfg.username or "", cfg.password or "", cfg.token_field)
    if err then return nil, err end

    local jwt_data, jerr = decode_jwt(token)
    if jerr then return nil, jerr end

    audit_claims(jwt_data, cfg, findings, severity)

    local alg = jwt_data.header.alg or "unknown"
    local sub = jwt_data.payload.sub or "n/a"
    local exp = jwt_data.payload.exp
    local summary = string.format("check_token: alg=%s sub=%s exp=%s findings=%d",
        alg, sub, exp and tostring(exp) or "none", #findings)
    local ok = #findings == 0
    return {
        passed   = ok,
        findings = as_array(findings),
        summary  = summary,
        level    = ok and "info" or "warn",
        alg      = alg,
    }, nil
end

-- audit_endpoint: verify that a protected endpoint correctly enforces JWT validation
local function run_audit_endpoint(cfg)
    local findings = {}
    local severity = cfg.severity or "high"
    local endpoint = cfg.endpoint or ""
    if endpoint == "" then return nil, "endpoint is required for audit_endpoint" end

    local valid_token, err = fetch_token(cfg.auth_url or "", cfg.username or "", cfg.password or "", cfg.token_field)
    if err then return nil, err end

    local httpc = http.new()
    httpc:set_timeout(10000)

    -- 1. no Authorization header → must be 401/403
    local res1, _ = httpc:request_uri(endpoint, {method = "GET"})
    if res1 and res1.status ~= 401 and res1.status ~= 403 then
        table.insert(findings, {
            label    = "jwt.endpoint.no_auth_bypass",
            severity = severity,
            detail   = string.format("endpoint returned %d with no Authorization header (expected 401/403)", res1.status),
        })
    end

    -- 2. tampered signature → must be 401/403
    local parts = {}
    for p in valid_token:gmatch("[^%.]+") do parts[#parts + 1] = p end
    if #parts == 3 then
        local tampered = parts[1] .. "." .. parts[2] .. ".invalidsignatureXXXXXXXXXX"
        local res2, _ = httpc:request_uri(endpoint, {
            method  = "GET",
            headers = {["Authorization"] = "Bearer " .. tampered},
        })
        if res2 and res2.status ~= 401 and res2.status ~= 403 then
            table.insert(findings, {
                label    = "jwt.endpoint.signature_bypass",
                severity = "critical",
                detail   = string.format("endpoint accepted a JWT with an invalid signature (status %d)", res2.status),
            })
        end
    end

    -- 3. valid token → must be 2xx
    local res3, _ = httpc:request_uri(endpoint, {
        method  = "GET",
        headers = {["Authorization"] = "Bearer " .. valid_token},
    })
    if not res3 or res3.status >= 400 then
        table.insert(findings, {
            label    = "jwt.endpoint.valid_token_rejected",
            severity = "medium",
            detail   = string.format("valid token was rejected (status %d)", res3 and res3.status or 0),
        })
    end

    local summary = string.format("audit_endpoint: endpoint=%s findings=%d", endpoint, #findings)
    local ok = #findings == 0
    return {
        passed   = ok,
        findings = as_array(findings),
        summary  = summary,
        level    = ok and "info" or "warn",
    }, nil
end

-- check_claims: fetches a token and checks its claims against expected values —
-- the issuer and audience it must carry, and scopes it must not. A token that
-- is structurally sound can still name the wrong issuer or grant a broader
-- scope than the service should ever hand out.
local function run_check_claims(cfg)
    local findings = {}
    local severity = cfg.severity or "high"

    local token, err = fetch_token(cfg.auth_url or "", cfg.username or "", cfg.password or "", cfg.token_field)
    if err then return nil, err end
    local jwt_data, jerr = decode_jwt(token)
    if jerr then return nil, jerr end
    local pld = jwt_data.payload

    if cfg.expected_issuer and cfg.expected_issuer ~= "" and pld.iss ~= cfg.expected_issuer then
        table.insert(findings, {
            label    = "jwt.claims.issuer_mismatch",
            severity = severity,
            detail   = string.format("iss is '%s', expected '%s'", tostring(pld.iss), cfg.expected_issuer),
        })
    end

    if cfg.expected_audience and cfg.expected_audience ~= "" then
        -- aud is a string or a list of strings.
        local aud, matched = pld.aud, false
        if type(aud) == "table" then
            for _, a in ipairs(aud) do if a == cfg.expected_audience then matched = true; break end end
        elseif aud == cfg.expected_audience then
            matched = true
        end
        if not matched then
            table.insert(findings, {
                label    = "jwt.claims.audience_mismatch",
                severity = severity,
                detail   = "aud does not include '" .. cfg.expected_audience .. "'",
            })
        end
    end

    -- Scopes the token must not carry. scope is space-delimited; scp is a list.
    local granted = {}
    if type(pld.scope) == "string" then
        for s in pld.scope:gmatch("%S+") do granted[s] = true end
    end
    if type(pld.scp) == "table" then
        for _, s in ipairs(pld.scp) do granted[s] = true end
    end
    for _, forbidden in ipairs(cfg.forbidden_scopes or {}) do
        if granted[forbidden] then
            table.insert(findings, {
                label    = "jwt.claims.forbidden_scope",
                severity = severity,
                detail   = "token carries scope '" .. forbidden .. "', which this credential should never be granted",
            })
        end
    end

    local ok = #findings == 0
    return {
        passed   = ok,
        findings = as_array(findings),
        summary  = string.format("check_claims: iss=%s aud=%s findings=%d",
            tostring(pld.iss or "none"), tostring(pld.aud or "none"), #findings),
        level    = ok and "info" or "warn",
    }, nil
end

-- audit_endpoints: runs the audit_endpoint checks over a list of endpoints, so
-- one step covers every protected route instead of one step per route. Each
-- endpoint's findings are prefixed with its URL.
local function run_audit_endpoints(cfg)
    local endpoints = cfg.endpoints or {}
    if #endpoints == 0 then return nil, "endpoints must be a non-empty list for audit_endpoints" end

    local findings, audited = {}, 0
    for _, endpoint in ipairs(endpoints) do
        local single = {}
        for k, v in pairs(cfg) do single[k] = v end
        single.endpoint = endpoint
        local result, err = run_audit_endpoint(single)
        if err then return nil, endpoint .. ": " .. err end
        audited = audited + 1
        local list = type(result.findings) == "table" and result.findings or {}
        for _, f in ipairs(list) do
            table.insert(findings, {label = f.label, severity = f.severity, detail = endpoint .. " — " .. f.detail})
        end
    end

    local ok = #findings == 0
    return {
        passed   = ok,
        findings = as_array(findings),
        summary  = string.format("audit_endpoints: %d endpoints, %d findings", audited, #findings),
        level    = ok and "info" or "warn",
    }, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then
        ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400)
    end

    local op  = payload.op or "check_token"
    local cfg = payload.config or {}

    if (cfg.auth_url or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "auth_url is required"})); return ngx.exit(400)
    end

    local result, err
    if op == "audit_endpoint" then
        result, err = run_audit_endpoint(cfg)
    elseif op == "check_claims" then
        result, err = run_check_claims(cfg)
    elseif op == "audit_endpoints" then
        result, err = run_audit_endpoints(cfg)
    else
        result, err = run_check_token(cfg)
    end

    if err then
        ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500)
    end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
