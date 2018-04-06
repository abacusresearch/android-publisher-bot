package main

import (
    "encoding/base64"
    "fmt"
    "github.com/nlopes/slack"
    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
    "golang.org/x/oauth2/jwt"
    "google.golang.org/api/androidpublisher/v2"
    "google.golang.org/api/googleapi"
    "io"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "os"
    "regexp"
    "strconv"
    "strings"
)

var rtm *slack.RTM

func addVersionCodeToPlayStoreTrack(
        publisher *androidpublisher.Service,
        edit *androidpublisher.AppEdit,
        track *androidpublisher.Track,
        appId string,
        appVersionCode int64,
        userFraction float64) bool {
    postSlackMessage("Adding version code *%v* to track *%v*.", appVersionCode, track.Track)

    track.UserFraction = userFraction
    track.VersionCodes = append(track.VersionCodes, appVersionCode)

    _, err := publisher.Edits.Tracks.
            Update(appId, edit.Id, track.Track, track).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot update the track: %v", err)
        return false
    }

    return true
}

func changeUserFraction(
        publisher *androidpublisher.Service,
        edit *androidpublisher.AppEdit,
        track *androidpublisher.Track,
        appId string,
        userFraction float64) bool {
    postSlackMessage("Changing user fraction for track *%v*.", track.Track)

    track.UserFraction = userFraction

    _, err := publisher.Edits.Tracks.
            Update(appId, edit.Id, track.Track, track).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot update the track: %v", err)
        return false
    }

    return true
}

func doDeploy(artifactId string, version string) {
    postSlackMessage("Ok, deploying *%v* with version *%v* ...", artifactId, version)

    artifactUrl := locateMavenArtifact(artifactId, version)
    artifactFile := downloadMavenArtifact(artifactUrl)

    if artifactFile == nil {
        return
    }

    defer os.Remove(artifactFile.Name())

    credentials := loadAndroidPublisherCredentials()

    if credentials == nil {
        return
    }

    client := credentials.Client(oauth2.NoContext)

    publisher, err := androidpublisher.New(client)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the publisher: %v", err)
        return
    }

    appId := fmt.Sprintf("%v.%v", getEnvironmentVariable("ANDROID_APP_ID_PREFIX"), artifactId)

    edit, err := publisher.Edits.
            Insert(appId, nil).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot insert the edit: %v", err)
        return
    }

    apk, err := publisher.Edits.Apks.
            Upload(appId, edit.Id).
            Media(artifactFile, googleapi.ContentType("application/vnd.android.package-archive")).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot upload the APK: %v", err)
        return
    }

    tracks, err := publisher.Edits.Tracks.
            List(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot list the tracks: %v", err)
        return
    }

    track := &androidpublisher.Track {Track: "internal"}

    for _, candidate := range tracks.Tracks {
        if (candidate.Track == "internal") {
            track = candidate
        }
    }

    // Remove the lower versions from the target track.

    if !removeAllVersionCodesFromPlayStoreTrack(publisher, edit, track, appId) {
        return
    }

    // Add the current version to the target track.

    if !addVersionCodeToPlayStoreTrack(publisher, edit, track, appId, apk.VersionCode, 0) {
        return
    }

    _, err = publisher.Edits.
            Commit(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot commit the edit: %v", err)
        return
    }

    postSlackMessage("Done.")
}

func doHalt(appId string, appVersionCode int64) {
    postSlackMessage("Ok, halting *%v* with version code *%v* ...", appId, appVersionCode)

    credentials := loadAndroidPublisherCredentials()

    if credentials == nil {
        return
    }

    client := credentials.Client(oauth2.NoContext)

    publisher, err := androidpublisher.New(client)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the publisher: %v", err)
        return
    }

    appId = fmt.Sprintf("%v.%v", getEnvironmentVariable("ANDROID_APP_ID_PREFIX"), appId)

    edit, err := publisher.Edits.
            Insert(appId, nil).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot insert the edit: %v", err)
        return
    }

    tracks, err := publisher.Edits.Tracks.
            List(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot list the tracks: %v", err)
        return
    }

    // Remove the version from all tracks.

    if !removeVersionCodeFromPlayStoreTracks(publisher, edit, tracks.Tracks, appId, appVersionCode) {
        return
    }

    _, err = publisher.Edits.
            Commit(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot commit the edit: %v", err)
        return
    }

    postSlackMessage("Done.")
}

func doHelp() {
    postSlackMessage("Sorry, I don't understand.")
}

func doPromote(appId string, appVersionCode int64, playStoreTrack string) {
    postSlackMessage("Ok, promoting *%v* with version code *%v* to track *%v* ...", appId, appVersionCode, playStoreTrack)

    credentials := loadAndroidPublisherCredentials()

    if credentials == nil {
        return
    }

    client := credentials.Client(oauth2.NoContext)

    publisher, err := androidpublisher.New(client)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the publisher: %v", err)
        return
    }

    appId = fmt.Sprintf("%v.%v", getEnvironmentVariable("ANDROID_APP_ID_PREFIX"), appId)

    edit, err := publisher.Edits.
            Insert(appId, nil).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot insert the edit: %v", err)
        return
    }

    tracks, err := publisher.Edits.Tracks.
            List(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot list the tracks: %v", err)
        return
    }

    track := &androidpublisher.Track {Track: playStoreTrack}

    for _, candidate := range tracks.Tracks {
        if (candidate.Track == playStoreTrack) {
            track = candidate;
        }
    }

    for _, candidate := range track.VersionCodes {
        if candidate == appVersionCode {
            postSlackMessage("Version code *%v* already exists in track *%v*.", appVersionCode, playStoreTrack)
            return
        }
    }

    // Remove all lower versions from the target track.

    if !removeAllVersionCodesFromPlayStoreTrack(publisher, edit, track, appId) {
       return
    }

    // Move the current version to the target tracks.

    if !removeVersionCodeFromPlayStoreTracks(publisher, edit, tracks.Tracks, appId, appVersionCode) {
        return
    }

    if !addVersionCodeToPlayStoreTrack(publisher, edit, track, appId, appVersionCode, 0) {
        return
    }

    _, err = publisher.Edits.
            Commit(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot commit the edit: %v", err)
        return
    }

    postSlackMessage("Done.")
}

func doRollout(appId string, appVersionCode int64, userPercentage int) {
    postSlackMessage("Ok, rolling out *%v* with version code *%v* to *%v%%* ...", appId, appVersionCode, userPercentage)

    credentials := loadAndroidPublisherCredentials()

    if credentials == nil {
        return
    }

    client := credentials.Client(oauth2.NoContext)

    publisher, err := androidpublisher.New(client)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the publisher: %v", err)
        return
    }

    appId = fmt.Sprintf("%v.%v", getEnvironmentVariable("ANDROID_APP_ID_PREFIX"), appId)

    edit, err := publisher.Edits.
            Insert(appId, nil).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot insert the edit: %v", err)
        return
    }

    tracks, err := publisher.Edits.Tracks.
            List(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot list the tracks: %v", err)
        return
    }

    track := &androidpublisher.Track {Track: "rollout"}

    for _, candidate := range tracks.Tracks {
        if (candidate.Track == "rollout") {
            track = candidate;
        }
    }

    exists := false

    for _, candidate := range track.VersionCodes {
        if candidate == appVersionCode {
            exists = true
        }
    }

    userFraction := float64(userPercentage) / 100

    if !exists {

        // Remove all lower versions from the target track.

        if !removeAllVersionCodesFromPlayStoreTrack(publisher, edit, track, appId) {
            return
        }

        // Move the current version to the target tracks.

        if !removeVersionCodeFromPlayStoreTracks(publisher, edit, tracks.Tracks, appId, appVersionCode) {
            return
        }

        if !addVersionCodeToPlayStoreTrack(publisher, edit, track, appId, appVersionCode, userFraction) {
            return
        }
    } else {

        // Change the user fraction.

        if !changeUserFraction(publisher, edit, track, appId, userFraction) {
            return
        }
    }

    _, err = publisher.Edits.
            Commit(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot commit the edit: %v", err)
        return
    }

    postSlackMessage("Done.")
}

func doShowTracks(appId string) {
    postSlackMessage("Ok, showing tracks for *%v* ...", appId)

    credentials := loadAndroidPublisherCredentials()

    if credentials == nil {
        return
    }

    client := credentials.Client(oauth2.NoContext)

    publisher, err := androidpublisher.New(client)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the publisher: %v", err)
        return
    }

    appId = fmt.Sprintf("%v.%v", getEnvironmentVariable("ANDROID_APP_ID_PREFIX"), appId)

    edit, err := publisher.Edits.
            Insert(appId, nil).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot insert the edit: %v", err)
        return
    }

    tracks, err := publisher.Edits.Tracks.
            List(appId, edit.Id).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot list the tracks: %v", err)
        return
    }

    for _, track := range tracks.Tracks {
        postSlackMessage("Track *%v* contains version codes *%v*.", track.Track, track.VersionCodes)
    }

    postSlackMessage("Done.")
}

func downloadMavenArtifact(url string) *os.File {
    client := &http.Client{}

    request, err := http.NewRequest("GET", url, nil)

    if err != nil {
        postSlackMessage("Sorry, I cannot create the HTTP request: %v", err)
        return nil
    }

    request.SetBasicAuth(
            getEnvironmentVariable("MAVEN_ACCOUNT_NAME"),
            getEnvironmentVariable("MAVEN_ACCOUNT_PASSWORD"))

    response, err := client.Do(request)

    if err != nil {
        postSlackMessage("Sorry, I cannot execute the HTTP request: %v", err)
        return nil
    }

    defer response.Body.Close()

    if response.StatusCode != 200 {
        postSlackMessage("Sorry, I didn't expect that HTTP status code: %v", response.StatusCode)
        return nil
    }

    result, err := ioutil.TempFile("", "")

    if err != nil {
        postSlackMessage("Sorry, I cannot create the temporary file: %v", err)
        return nil
    }

    _, err = io.Copy(result, response.Body)

    if err != nil {
        postSlackMessage("Sorry, I cannot write the temporary file: %v", err)
        return nil
    }

    _, err = result.Seek(0, 0)

    if err != nil {
        postSlackMessage("Sorry, I cannot seek in the temporary file: %v", err)
        return nil
    }

    return result
}

func getEnvironmentVariable(name string) string {
    result := os.Getenv(name)

    if len(result) == 0 {
        log.Fatalf("Sorry, I cannot get the environment variable: %v", name)
    }

    return result
}

func handleSlackMessage(event *slack.MessageEvent) {
    text := event.Msg.Text

    log.Printf("#%v %v", event.Channel, text)

    if event.Channel != getEnvironmentVariable("SLACK_BOT_CHANNEL_ID") {
        return
    }

    textPrefix := fmt.Sprintf("<@%s>", getEnvironmentVariable("SLACK_BOT_USER_ID"))

    if !strings.HasPrefix(text, textPrefix) {
        return
    }

    // Handle the 'deploy' command.

    command := regexp.
            MustCompile("<[^>]+> +deploy +([^ ]+) +([^ ]+)").
            FindStringSubmatch(text)

    if len(command) > 0 {
        doDeploy(command[1], command[2])
        return
    }

    // Handle the 'halt' command.

    command = regexp.
            MustCompile("<[^>]+> +halt +([^ ]+) +([^ ]+)").
            FindStringSubmatch(text)

    if len(command) > 0 {
        appVersionCode, err := strconv.ParseInt(command[2], 10, 64)

        if err != nil {
            postSlackMessage("Sorry, I don't understand that version code.")
            return
        }

        doHalt(command[1], appVersionCode)
        return
    }

    // Handle the 'promote' command.

    command = regexp.
            MustCompile("<[^>]+> +promote +([^ ]+) +([^ ]+) +to +(.*)").
            FindStringSubmatch(text)

    if len(command) > 0 {
        appVersionCode, err := strconv.ParseInt(command[2], 10, 64)

        if err != nil {
            postSlackMessage("Sorry, I don't understand that version code.")
            return
        }

        doPromote(command[1], appVersionCode, command[3])
        return
    }

    // Handle the 'rollout' command.

    command = regexp.
            MustCompile("<[^>]+> +rollout +([^ ]+) +([^ ]+) +to +(.*)%").
            FindStringSubmatch(text)

    if len(command) > 0 {
        appVersionCode, err := strconv.ParseInt(command[2], 10, 64)

        if err != nil {
            postSlackMessage("Sorry, I don't understand that version code.")
            return
        }

        userPercentage, err := strconv.Atoi(command[3])

        if err != nil {
            postSlackMessage("Sorry, I don't understand that user percentage.")
            return
        }

        doRollout(command[1], appVersionCode, userPercentage)
        return
    }

    // Handle the 'show tracks' command.

    command = regexp.
            MustCompile("<[^>]+> +show +tracks +for +(.+)").
            FindStringSubmatch(text)

    if len(command) > 0 {
        doShowTracks(command[1])
        return
    }

    doHelp()
}

func handleSlackMessages() {
    client := slack.New(getEnvironmentVariable("SLACK_BOT_TOKEN"))

    rtm = client.NewRTM()

    go rtm.ManageConnection()

    for event := range rtm.IncomingEvents {
        switch typedEvent := event.Data.(type) {
        case *slack.MessageEvent:
            handleSlackMessage(typedEvent)
        }
    }
}

func loadAndroidPublisherCredentials() *jwt.Config {
    data, err := base64.StdEncoding.DecodeString(getEnvironmentVariable("ANDROID_PUBLISHER_CREDENTIALS"))

    if err != nil {
        postSlackMessage("Sorry, I cannot decode the credentials: %v", err)
        return nil
    }

    result, err := google.JWTConfigFromJSON(
            data,
            "https://www.googleapis.com/auth/androidpublisher")

    if err != nil {
        postSlackMessage("Sorry, I cannot parse the credentials: %v", err)
        return nil
    }

    return result
}

func locateMavenArtifact(artifactId string, version string) string {
    var result strings.Builder

    artifactId = url.PathEscape(artifactId)
    version = url.PathEscape(version)

    result.WriteString(getEnvironmentVariable("MAVEN_REPOSITORY"))
    result.WriteString(strings.Replace(getEnvironmentVariable("MAVEN_GROUP_ID"), ".", "/", -1))
    result.WriteString("/")
    result.WriteString(artifactId)
    result.WriteString("/")
    result.WriteString(version)
    result.WriteString("/")
    result.WriteString(artifactId + "-" + version + ".apk")

    return result.String()
}

func main() {
    handleSlackMessages()
}

func postSlackMessage(message string, arguments ...interface{}) {
    _, _, err := rtm.PostMessage(
            getEnvironmentVariable("SLACK_BOT_CHANNEL_ID"),
            fmt.Sprintf(message, arguments...),
            slack.NewPostMessageParameters())

    if err != nil {
        log.Fatalf("Sorry, I cannot post the message: %v", err)
    }
}

func removeAllVersionCodesFromPlayStoreTrack(
        publisher *androidpublisher.Service,
        edit *androidpublisher.AppEdit,
        track *androidpublisher.Track,
        appId string) bool {
    for _, versionCode := range track.VersionCodes {
        postSlackMessage("Removing version code *%v* from track *%v*.", versionCode, track.Track)
    }

    track.VersionCodes = []int64 {}

    _, err := publisher.Edits.Tracks.
            Update(appId, edit.Id, track.Track, track).
            Do()

    if err != nil {
        postSlackMessage("Sorry, I cannot update the track: %v", err)
        return false
    }

    return true
}

func removeVersionCodeFromPlayStoreTracks(
        publisher *androidpublisher.Service,
        edit *androidpublisher.AppEdit,
        tracks []*androidpublisher.Track,
        appId string,
        appVersionCode int64) bool {
    for _, track := range tracks {
        var appVersionCodes []int64

        for _, candidate := range track.VersionCodes {
            if candidate == appVersionCode {
                postSlackMessage("Removing version code *%v* from track *%v*.", candidate, track.Track)
            } else {
                appVersionCodes = append(appVersionCodes, candidate)
            }
        }

        track.VersionCodes = appVersionCodes

        _, err := publisher.Edits.Tracks.
                Update(appId, edit.Id, track.Track, track).
                Do()

        if err != nil {
            postSlackMessage("Sorry, I cannot update the track: %v", err)
            return false
        }
    }

    return true
}
