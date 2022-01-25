package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lf-edge/eve/api/go/profile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	contentType = "Content-Type"
	mimeProto   = "application/x-proto-binary"
)

var (
	profileFile = flag.String("profile", "/mnt/profile",
		"File with current profile")
	radioSilenceCfgFile = flag.String("radio-silence", "/mnt/radio-silence",
		"File with the requested radio-silence state ('OFF'/'ON' or '0'/'1')")
	radioSilenceCounterFile = flag.String("radio-silence-counter", "/mnt/radio-silence-counter",
		"File contains the number of radio-silence state changes (ON/OFF switches) already performed")
	radioStatusFile = flag.String("radio-status", "/mnt/radio-status.json",
		"Periodically updated JSON file with the current radio status")
	appInfoFile = flag.String("app-info-status", "/mnt/app-info-status.json",
		"File to save app info status")
	token = flag.String("token", "", "Token of profile server")
)

var (
	radioSilenceIsChanging bool
	radioSilenceCounter    int
	radioSilenceMTime      time.Time
)

func main() {
	flag.Parse()
	http.HandleFunc("/api/v1/local_profile", localProfile)
	http.HandleFunc("/api/v1/radio", radio)
	http.HandleFunc("/api/v1/appinfo", appinfo)
	fmt.Println(http.ListenAndServe(":8888", nil))
}

func appinfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		errStr := fmt.Sprintf("Unexpected method: %s", r.Method)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusMethodNotAllowed)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errStr := fmt.Sprintf("Failed to read request body: %v", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	appInfoList := &profile.LocalAppInfoList{}
	err = proto.Unmarshal(body, appInfoList)
	if err != nil {
		errStr := fmt.Sprintf("Failed to unmarshal request body: %v", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	data, err := protojson.Marshal(appInfoList)
	if err != nil {
		errStr := fmt.Sprintf("Marshal: %s", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}
	err = ioutil.WriteFile(*appInfoFile, data, 0644)
	if err != nil {
		errStr := fmt.Sprintf("Failed to write request body: %v", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func localProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		errStr := fmt.Sprintf("Unexpected method: %s", r.Method)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusMethodNotAllowed)
		return
	}
	profileFromFile, err := ioutil.ReadFile(*profileFile)
	if err != nil {
		errStr := fmt.Sprintf("ReadFile: %s", err)
		fmt.Println(errStr)
		if os.IsNotExist(err) {
			http.Error(w, errStr, http.StatusNotFound)
		} else {
			http.Error(w, errStr, http.StatusInternalServerError)
		}
		return
	}
	localProfileObject := &profile.LocalProfile{
		LocalProfile: strings.TrimSpace(string(profileFromFile)),
		ServerToken:  *token,
	}
	data, err := proto.Marshal(localProfileObject)
	if err != nil {
		errStr := fmt.Sprintf("Marshal: %s", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}
	w.Header().Set(contentType, mimeProto)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		fmt.Printf("Failed to write: %s\n", err)
	}
}

func radio(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		errStr := fmt.Sprintf("Unexpected method: %s", r.Method)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusMethodNotAllowed)
		return
	}

	// Publish received radio status into the file.
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errStr := fmt.Sprintf("Failed to read request body: %v", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	radioStatus := &profile.RadioStatus{}
	err = proto.Unmarshal(body, radioStatus)
	if err != nil {
		errStr := fmt.Sprintf("Failed to unmarshal request body: %v", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(radioStatus)
	if err != nil {
		errStr := fmt.Sprintf("Marshal: %s", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}
	err = ioutil.WriteFile(*radioStatusFile, data, 0644)
	if err != nil {
		errStr := fmt.Sprintf("WriteFile: %s", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}

	// Update radio-silence-counter file.
	if radioSilenceIsChanging {
		// radio-silence was switched ON or OFF
		radioSilenceCounter++
		data := []byte(fmt.Sprintf("%d", radioSilenceCounter))
		err := ioutil.WriteFile(*radioSilenceCounterFile, data, 0644)
		if err != nil {
			errStr := fmt.Sprintf("WriteFile: %s", err)
			fmt.Println(errStr)
		}
		radioSilenceIsChanging = false
	}

	// If the requested radio-silence state has changed, send it in the response.
	info, err := os.Stat(*radioSilenceCfgFile)
	if err != nil {
		errStr := fmt.Sprintf("Stat: %s", err)
		fmt.Println(errStr)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if info.ModTime().Equal(radioSilenceMTime) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	radioSilenceMTime = info.ModTime()
	data, err = ioutil.ReadFile(*radioSilenceCfgFile)
	if err != nil {
		errStr := fmt.Sprintf("ReadFile: %s", err)
		fmt.Println(errStr)
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}
	radioSilenceConfig := strings.ToLower(strings.TrimSpace(string(data)))
	radioConfig := &profile.RadioConfig{
		RadioSilence: radioSilenceConfig == "on" || radioSilenceConfig == "1",
		ServerToken:  *token,
	}
	data, err = proto.Marshal(radioConfig)
	if err != nil {
		errStr := fmt.Sprintf("Marshal: %s", err)
		fmt.Println(errStr)
		http.Error(w, errStr, http.StatusInternalServerError)
		return
	}
	w.Header().Set(contentType, mimeProto)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		fmt.Printf("Failed to write: %s\n", err)
	} else {
		radioSilenceIsChanging = radioStatus.RadioSilence != radioConfig.RadioSilence
	}
}
