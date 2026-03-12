# Custom Key Extraction

Customize how rate limit keys are derived from requests.

## Built-in Key Functions

```go
// By IP (default)
capacitor.WithKeyFunc(capacitor.KeyFromRemoteIP)

// By header value
capacitor.WithKeyFunc(capacitor.KeyFromHeader("X-API-Key"))

// By query parameter
keyFunc := func(r *http.Request) string {
    return r.URL.Query().Get("api_key")
}
capacitor.WithKeyFunc(keyFunc)
```

## Skip Rate Limiting

Return empty string to skip rate limiting:

```go
capacitor.WithKeyFunc(func(r *http.Request) string {
    // Skip health checks
    if r.URL.Path == "/health" {
        return ""
    }
    
    // Skip internal services
    if r.Header.Get("X-Internal") == "true" {
        return ""
    }
    
    // Default: use API key or IP
    if key := r.Header.Get("X-API-Key"); key != "" {
        return key
    }
    return r.RemoteAddr
})
```

## Complex Key Functions

```go
// Rate limit by user + endpoint combination
keyFunc := func(r *http.Request) string {
    userID := r.Header.Get("X-User-ID")
    if userID == "" {
        return ""
    }
    
    // Group by endpoint type
    endpoint := "default"
    switch {
    case strings.HasPrefix(r.URL.Path, "/api/admin"):
        endpoint = "admin"
    case strings.HasPrefix(r.URL.Path, "/api/write"):
        endpoint = "write"
    }
    
    return fmt.Sprintf("%s:%s", userID, endpoint)
}

rl := capacitor.NewMiddleware(limiter,
    capacitor.WithKeyFunc(keyFunc),
)
```

## Using Path Parameters

```go
// For routers like chi or gorilla/mux
keyFunc := func(r *http.Request) string {
    // Extract user ID from path /users/{id}
    path := r.URL.Path
    if strings.HasPrefix(path, "/users/") {
        parts := strings.Split(path, "/")
        if len(parts) >= 3 {
            return "user:" + parts[2]
        }
    }
    return r.RemoteAddr
}
```

## Combined Keys

```go
// Rate limit by API key, but also by IP to prevent key sharing
keyFunc := func(r *http.Request) string {
    apiKey := r.Header.Get("X-API-Key")
    ip := r.RemoteAddr
    
    // Use both for composite key
    if apiKey != "" {
        return fmt.Sprintf("key:%s-ip:%s", apiKey, ip)
    }
    return ip
}
```

## Using Context

```go
// Set key in context from authentication middleware
keyFunc := func(r *http.Request) string {
    if userID := r.Context().Value("user_id"); userID != nil {
        return fmt.Sprintf("user:%v", userID)
    }
    return r.RemoteAddr
}
```
