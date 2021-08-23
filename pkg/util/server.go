package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	admission "k8s.io/api/admission/v1beta1"
	core "k8s.io/api/core/v1"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	matchingModulus int
	proxyUrl string
	namespaceCache []string
	nsMutex sync.Mutex
)

func knownNamespace( namespace string ) bool {
	nsMutex.Lock()
	defer nsMutex.Unlock()
	for _, v := range namespaceCache {
		if v == namespace {
			log.Println("namespace " + namespace + " is known")
			return true
		}
	}
	log.Println("namespace " + namespace + " is not known")
	return false
}

func addKnownNamespace( namespace string ) {
	nsMutex.Lock()
	defer nsMutex.Unlock()
	log.Println("namespace " + namespace + " is now known")
	namespaceCache = append(namespaceCache, namespace)
	if len(namespaceCache) > 20 {
		namespaceCache = namespaceCache[1:]
		log.Println("more than 20 namespaces found trimming oldest namespace["+string(len(namespaceCache))+"]")
	}
}

// getContainerEnvMap returns an array where the index is the container index and
// the boolean is true if an `env` element is to be added to the json. an error
// is returned if the admission is not one we are interested in.
func getContainerEnvMap( review admission.AdmissionReview ) ([]int, error) {
	var pod core.Pod

	if !strings.HasPrefix(review.Request.Namespace,"ci-op-")  {
		return nil, errors.New("namespace not matched\n")
	}

	err := json.Unmarshal(review.Request.Object.Raw,&pod)
	if err != nil {
		return nil, err
	}

	if val, ok := pod.Labels["ci.openshift.io/metadata.target"]; ok{
		if val != "e2e-vsphere" &&
			val != "e2e-vsphere-ovn" &&
			val != "e2e-vsphere-csi" &&
			val != "e2e-vsphere-serial"{
			return nil, errors.New("not a matching prow target\n")
		}
	} else {
		return nil, errors.New("pod is missing target label\n")
	}

	containers := pod.Spec.Containers
	if len(containers) == 0 {
		return nil, errors.New("no containers found in pod\n")
	}

	admissionOfInterest := false
	var envCreateMap []int
	for _, container := range containers {
		envCreateMap = append(envCreateMap, len(container.Env))
		if admissionOfInterest == false {
			for _, envVar := range container.Env {
				if envVar.Name == "LEASED_RESOURCE" {
					log.Printf("pod in %s has a leased resource variable %s\n", review.Request.Namespace, envVar.Value)
					splits := strings.Split(envVar.Value, "-")
					if knownNamespace(review.Request.Namespace) {
						admissionOfInterest = true
						break
					} else {
						if len(splits) != 3 {
							return nil, errors.New("`LEASED_RESOURCE` invalid format. should be `ci-segment-`\n")
						}

						if envVar.Value != "ci-segment-76" &&
							envVar.Value != "ci-segment-80" &&
							envVar.Value != "ci-segment-84" {
							log.Println("not a matching segment")
							continue
						}

						segment, err := strconv.Atoi(splits[2])
						if err != nil {
							return nil, errors.New("`ci-segment-` should end with integer\n")
						}
						if segment%matchingModulus != 0 {
							return nil, errors.New("`ci-segment-` does not match modulus filter\n")
						}

						// check to see if the ibm cloud credentials are mounted
						for _, volume := range container.VolumeMounts {
								if volume.Name == "test-credentials-ci-ibmcloud" {
								admissionOfInterest = true
								addKnownNamespace(review.Request.Namespace)
								break
							}
						}
						log.Println("cloud credential volume not found in container")
					}
				}
			}
		}
	}
	if admissionOfInterest {
		return envCreateMap, nil
	}
	return nil, errors.New("pod in " + pod.Namespace + " does not receive proxy vars\n")
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
	containers, err := getContainerEnvMap(review)
	if err == nil {
		patchContent := "["
		for index, val := range containers {
			if index != 0 {
				patchContent = patchContent + ","
			}
			if val == 0 {
				patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env\", \"value\": []},", index)
				log.Printf("adding environment variable")
			}
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env/-\", \"value\": {\"name\":\"HTTP_PROXY\",\"value\":\"%s\"}},", index, proxyUrl)
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env/-\", \"value\": {\"name\":\"HTTPS_PROXY\",\"value\":\"%s\"}},", index, proxyUrl)
			patchContent = patchContent + fmt.Sprintf("{\"op\": \"add\", \"path\": \"/spec/containers/%d/env/-\", \"value\": {\"name\":\"NO_PROXY\",\"value\":\"vcenter.sddc-44-236-21-251.vmwarevmc.com,.svc,.cluster.local,172.30.0.0/16\"}}", index)
		}
		patchContent = patchContent +"]"
		log.Printf("patch length %d", len(patchContent))
		admissionResult.Patch = []byte(patchContent)
		admissionResult.PatchType = &patchType
	} else {
		log.Printf("no proxy injection. " + err.Error())
	}

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

	log.Printf("webhook setup aug231444\n")
	err := http.ListenAndServeTLS(":8443", "/var/run/secrets/webhook/tls.crt", "/var/run/secrets/webhook/tls.key", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
