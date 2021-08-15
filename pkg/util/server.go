package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	admission "k8s.io/api/admission/v1beta1"
	core "k8s.io/api/core/v1"
	"os"
	"strconv"
	"strings"

	"log"
	"net/http"
)

var (
	matchingModulus int
	proxyUrl string
)
func checkIfPodOfInterest ( review admission.AdmissionReview ) (int, error) {
	var pod core.Pod
	if !strings.HasPrefix(review.Request.Namespace,"ci-op-")  {
		return -1, errors.New("namespace not matched\n")
	}

	err := json.Unmarshal(review.Request.Object.Raw,&pod)
	if err != nil {
		return -1, err
	}

	if val, ok := pod.Labels["ci.openshift.io/metadata.target"]; ok{
		if !strings.HasPrefix(val,"e2e-vsphere") {
			return -1, errors.New("not a matching pod\n")
		}
	} else {
		fmt.Printf("Pod labels: %v\n", pod.Labels)
		return -1, errors.New("pod is missing label\n")
	}

	containers := pod.Spec.Containers
	if len(containers) == 0 {
		return -1, errors.New("no containers found in pod\n")
	}

	for _, container := range containers {
		for _, envVar := range container.Env {
			if envVar.Name == "LEASED_RESOURCE" {
				fmt.Printf("pod %s has a leased resource variable! %s\n", pod.Name, envVar.Value)
				splits := strings.Split(envVar.Value,"-")
				if len(splits) != 3 {
					return -1, errors.New("`LEASED_RESOURCE` invalid format. should be `ci-segment-`\n")
				}
				segment, err := strconv.Atoi(splits[2])
				if err != nil {
					return -1, errors.New("`ci-segment-` should end with integer\n")
				}
				if segment % matchingModulus != 0 {
					return -1, errors.New("`ci-segment-` does not match modulus filter\n")
				}
				return len(containers),nil
			}
		}
	}
	return -1, errors.New("no containers with LEASED_RESOURCE found\n")
}

func WebHookHandler(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var review admission.AdmissionReview
	json.Unmarshal(body, &review)

	var patchType = admission.PatchTypeJSONPatch
	admissionResult := admission.AdmissionResponse{
		UID:              review.Request.UID,
		Allowed:          true,
		Result:           nil,
		Patch:            nil,
		PatchType:        nil,
		AuditAnnotations: nil,
		Warnings:         nil,
	}
	containers, err := checkIfPodOfInterest(review)
	if err == nil {
		patchContent := "["
		for cindex := 0; cindex < containers; cindex++ {
			if cindex != 0 {
				patchContent = patchContent + ","
			}
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env\", \"value\": []},", cindex)
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env/-\", \"value\": {\"name\":\"HTTP_PROXY\",\"value\":\"%s\"}},", cindex, proxyUrl)
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env/-\", \"value\": {\"name\":\"HTTPS_PROXY\",\"value\":\"%s\"}}", cindex, proxyUrl)
		}
		patchContent = patchContent +"]"

		admissionResult.Patch = []byte(patchContent)
		admissionResult.PatchType = &patchType
	} else {
		fmt.Printf(err.Error())
	}

	fmt.Printf("Response: %s\n" , string(admissionResult.Patch))

	review.Response = &admissionResult
	response, err := json.Marshal(review)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

func SetupWebhookListener() {
	val := os.Getenv("PROXY_URL")
	if val == "" {
		fmt.Printf("PROXY_URL must be defined")
		return
	} else {
		proxyUrl = val
	}
	val = os.Getenv("MATCHING_MODULUS")
	matchingModulus = 4
	if val != "" {
		setting, err := strconv.Atoi(val)
		if err != nil {
			fmt.Printf("MATCHING_MODULUS should be integer. Using default of " + string(matchingModulus))
		} else {
			matchingModulus = setting
		}
	}
	http.HandleFunc("/", WebHookHandler)

	fmt.Printf("Webhook setup!\n")
	err := http.ListenAndServeTLS(":8443", "/var/run/secrets/webhook/tls.crt", "/var/run/secrets/webhook/tls.key", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
