package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "plex-discord-rpc/discordrpc"
    "plex-discord-rpc/mpvipc"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"
)

const (
    largeImageKey  = "logo"
    largeImageText = "Plex"

    imagePlay  = "play"
    imagePause = "pause"
    imageIdle  = "stop"

    pollInterval  = time.Second
    retryInterval = 500 * time.Millisecond
)

type plexMediaItem struct {
    URL      string `json:"url"`
    Type     string `json:"type"`
    Decision struct {
        MetadataItem struct {
            Title            string `json:"title"`
            ServerEntityID   string `json:"serverEntityID"`
            Guid             string `json:"guid"`
            GrandparentGuid  string `json:"grandparentGuid"`
            Thumb            string `json:"thumb"`
            Index            int    `json:"index"`
            Type             string `json:"type"`
            IsAdult          bool   `json:"isAdult"`
            GrandparentTitle string `json:"grandparentTitle"`
            GrandparentThumb string `json:"grandparentThumb"`
            ParentThumb      string `json:"parentThumb"`
            ParentIndex      int    `json:"parentIndex"`
        } `json:"metadataItem"`
    } `json:"decision"`
}

type mediaInfo struct {
    title       string
    contentType string
    mediaType   string
    showTitle   string
    showSeason  int
    showEpisode int
    thumbURL    string
    paused      bool
    idle        bool
    timePos     float64
    duration    float64
}

type plexResource struct {
    Name             string `json:"name"`
    ClientIdentifier string `json:"clientIdentifier"`
    AccessToken      string `json:"accessToken"`
    Provides         string `json:"provides"`
    Connections      []struct {
        URI   string `json:"uri"`
        Local bool   `json:"local"`
        Relay bool   `json:"relay"`
    } `json:"connections"`
}

type plexMetadataResponse struct {
    MediaContainer struct {
        Metadata []struct {
            Thumb string `json:"thumb"`
        } `json:"Metadata"`
    } `json:"MediaContainer"`
}

var (
    client   *mpvipc.Client
    presence *discordrpc.Presence

    thumbURLCache = make(map[string]cachedThumb)
    thumbCacheMux sync.Mutex
    httpClient    = &http.Client{Timeout: 5 * time.Second}
)

type cachedThumb struct {
    url    string
    expiry time.Time
}

func pruneExpiredThumbCache(now time.Time) {
    for key, entry := range thumbURLCache {
        if entry.expiry.Before(now) {
            delete(thumbURLCache, key)
        }
    }
}

func getMediaInfo() (info mediaInfo, err error) {
    // funcs
    getString := func(key string) (prop string) {
        prop, err = client.GetPropertyString(key)
        if err != nil {
            log.Println("GetPropertyString error:", err)
            return ""
        }
        return
    }
    getProperty := func(key string) (prop interface{}) {
        prop, err = client.GetProperty(key)
        if err != nil {
            log.Println("GetProperty error:", err)
            return nil
        }
        return
    }

    // mpv properties
    property := getProperty("pause")
    if property != nil {
        if b, ok := property.(bool); ok {
            info.paused = b
        }
    }

    property = getProperty("time-pos")
    if property != nil {
        switch val := property.(type) {
        case float64:
            info.timePos = val
        case int:
            info.timePos = float64(val)
        case string:
            float, err := strconv.ParseFloat(val, 64)
            if err == nil {
                info.timePos = float
            }
        }
    }

    property = getProperty("idle-active")
    if property != nil {
        if b, ok := property.(bool); ok {
            info.idle = b
        }
    }

    property = getProperty("duration")
    if property != nil {
        switch val := property.(type) {
        case float64:
            info.duration = val
        case int:
            info.duration = float64(val)
        case string:
            float, err := strconv.ParseFloat(val, 64)
            if err == nil {
                info.duration = float
            }
        }
    }

    // plex
    var media plexMediaItem
    plexUAT := strings.Trim(getString("user-data/plex/user-access-token"), "\"")
    if err != nil {
        return
    }
    plexClientID := strings.Trim(getString("user-data/plex/client-id"), "\"")
    if err != nil {
        return
    }
    mediaString := getString("user-data/plex/playing-media")
    if err != nil {
        return
    }
    if mediaString == "" {
        info.idle = true
        return
    }
    err = json.Unmarshal([]byte(mediaString), &media)
    if err != nil {
        return
    }
    if media == (plexMediaItem{}) {
        info.idle = true
        return
    }
    metadataItem := media.Decision.MetadataItem

    info.contentType = media.Type
    info.mediaType = metadataItem.Type
    switch metadataItem.Type {
    case "episode":
        info.title = metadataItem.GrandparentTitle
        info.showSeason = metadataItem.ParentIndex
        info.showEpisode = metadataItem.Index
        info.showTitle = metadataItem.Title
    case "movie":
        info.title = metadataItem.Title
    default:
        info.title = metadataItem.Title
    }

    if media.URL != "" && plexUAT != "" && plexClientID != "" && !metadataItem.IsAdult {
        var thumbPath string
        switch metadataItem.Type {
        case "episode":
            if metadataItem.ParentThumb != "" {
                thumbPath = metadataItem.ParentThumb
            } else if metadataItem.GrandparentThumb != "" {
                thumbPath = metadataItem.GrandparentThumb
            }
        case "movie":
            thumbPath = metadataItem.Thumb
        default:
            thumbPath = metadataItem.Thumb
        }
        if thumbPath != "" {
            // Prefer the public Plex metadata provider so no access token is exposed in URLs.
            lookupGuid := metadataItem.Guid
            if metadataItem.Type == "episode" && metadataItem.GrandparentGuid != "" {
                lookupGuid = metadataItem.GrandparentGuid
            }
            if lookupGuid != "" {
                info.thumbURL = getPublicThumbURL(lookupGuid, plexUAT, plexClientID)
            }
        }
    }

    return
}

// getPublicThumbURL tries to fetch artwork from Plex's public metadata provider
// (metadata.provider.plex.tv). On success it returns a CDN URL served from
// metadata-static.plex.tv — no server access token is embedded in that URL.
func getPublicThumbURL(guid string, plexUAT string, plexClientID string) string {
    if !strings.HasPrefix(guid, "plex://") {
        return ""
    }

    cacheKey := guid
    now := time.Now()
    thumbCacheMux.Lock()
    pruneExpiredThumbCache(now)
    if cached, ok := thumbURLCache[cacheKey]; ok && now.Before(cached.expiry) {
        thumbCacheMux.Unlock()
        return cached.url
    }
    thumbCacheMux.Unlock()

    parts := strings.Split(guid, "/")
    if len(parts) < 3 {
        return ""
    }
    id := parts[len(parts)-1]

    req, e := http.NewRequest("GET", "https://metadata.provider.plex.tv/library/metadata/"+id, nil)
    if e != nil {
        return ""
    }
    req.Header.Set("Accept", "application/json")
    req.Header.Set("X-Plex-Product", largeImageText)
    req.Header.Set("X-Plex-Token", plexUAT)
    req.Header.Set("X-Plex-Client-Identifier", plexClientID)

    res, e := httpClient.Do(req)
    if e != nil {
        return ""
    }
    defer res.Body.Close()
    if res.StatusCode != http.StatusOK {
        return ""
    }

    var metadata plexMetadataResponse
    if e = json.NewDecoder(res.Body).Decode(&metadata); e != nil {
        return ""
    }

    if len(metadata.MediaContainer.Metadata) > 0 {
        thumb := metadata.MediaContainer.Metadata[0].Thumb
        thumbURL := ""
        if strings.HasPrefix(thumb, "http") {
            thumbURL = thumb
        } else if strings.HasPrefix(thumb, "/") {
            thumbURL = "https://metadata-static.plex.tv" + thumb
        }
        if thumbURL != "" {
            thumbCacheMux.Lock()
            thumbURLCache[cacheKey] = cachedThumb{
                url:    thumbURL,
                expiry: time.Now().Add(10 * time.Minute),
            }
            thumbCacheMux.Unlock()
            return thumbURL
        }
    }
    return ""
}

func getActivity() (activity discordrpc.Activity, err error) {
    mediaInfo, err := getMediaInfo()
    if err != nil {
        fmt.Println("getMediaInfo Error:", err)
        return
    }

    activity.Type = 3

    // Idle
    if mediaInfo.idle {
        activity.SmallImageKey = imageIdle
        activity.LargeImageKey = largeImageKey
        activity.LargeImageText = largeImageText
        activity.Details = "Nothing Playing"
        activity.SmallImageText = "Nothing Playing"
        activity.State = ""
        return
    }

    // Large Image
    if mediaInfo.thumbURL != "" {
        activity.LargeImageKey = mediaInfo.thumbURL
    } else {
        activity.LargeImageKey = largeImageKey
    }

    // Details
    if mediaInfo.mediaType == "episode" && mediaInfo.showTitle != "" {
        activity.LargeImageText = mediaInfo.showTitle
        activity.Details = mediaInfo.title
    } else if mediaInfo.mediaType == "movie" && mediaInfo.title != "" {
        activity.LargeImageText = mediaInfo.title
        activity.Details = mediaInfo.title
    } else {
        activity.LargeImageText = largeImageText
        activity.Details = "Nothing Playing"
    }

    // State
    if mediaInfo.mediaType == "episode" && mediaInfo.showTitle != "" {
        activity.State = fmt.Sprintf("S%02dE%02d - %s", mediaInfo.showSeason, mediaInfo.showEpisode, mediaInfo.showTitle)
    } else if mediaInfo.mediaType == "movie" && mediaInfo.title != "" {
        activity.State = ""
    } else {
        activity.State = ""
    }

    // Small Image
    if mediaInfo.paused {
        activity.SmallImageKey = imagePause
        activity.SmallImageText = "Paused"
    } else {
        activity.SmallImageKey = imagePlay
        activity.SmallImageText = "Playing"
    }

    // Timestamps
    if !mediaInfo.paused && mediaInfo.duration > 0 && mediaInfo.timePos >= 0 {
        now := time.Now().UnixMilli()
        startTimePos := now - (int64(mediaInfo.timePos) * 1000)
        duration := startTimePos + (int64(mediaInfo.duration) * 1000)
        activity.Timestamps = &discordrpc.ActivityTimestamps{Start: startTimePos, End: duration}
    }

    return
}

func openClient() {
    err := client.Open(os.Args[1])
    if err != nil {
        log.Fatalln(err)
    }
    log.Println("(mpv-ipc): connected")
}

func openPresence() {
    for range time.Tick(500 * time.Millisecond) {
        if client.IsClosed() {
            return
        }
        err := presence.Open()
        if err == nil {
            break
        }
    }
    log.Println("(discord-rpc): connected")
}

func init() {
    log.SetOutput(os.Stdout)
    log.SetFlags(log.Lmsgprefix)

    client = mpvipc.NewClient()
    presence = discordrpc.NewPresence(os.Args[2])
}

func main() {
    defer func() {
        if !client.IsClosed() {
            err := client.Close()
            if err != nil {
                log.Fatalln(err)
            }
            log.Println("(mpv-ipc): disconnected")
        }
        if !presence.IsClosed() {
            err := presence.Close()
            if err != nil {
                log.Fatalln(err)
            }
            log.Println("(discord-rpc): disconnected")
        }
    }()

    openClient()
    go openPresence()

    for range time.Tick(time.Second) {
        if client.IsClosed() {
            return
        }
        activity, err := getActivity()
        if err != nil {
            if errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) {
                return
            }
            log.Println(err)
            continue
        }
        if !presence.IsClosed() {
            go func(a discordrpc.Activity) {
                err := presence.Update(a)
                if err != nil {
                    if errors.Is(err, syscall.EPIPE) {
                        err = presence.Close()
                        if err != nil {
                            log.Fatalln(err)
                        }
                        log.Println("(discord-rpc): reconnecting...")
                        go openPresence()
                    } else if !errors.Is(err, io.EOF) {
                        log.Println(err)
                    }
                }
            }(activity)
        }
    }
}
