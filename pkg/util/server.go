package util

import (
	"encoding/json"
	"io/ioutil"
	admission "k8s.io/api/admission/v1beta1"
	"log"
	"net/http"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func WebHookHandler(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var review admission.AdmissionReview
	json.Unmarshal(body, &review)

	patchJSON := []byte(`[
		{"op": "add", "path": "/spec/containers/0/env", "value": []},
		{"op": "add", "path": "/spec/containers/0/env/-", "value": {"name":"HTTPS_PROXY","value":"http://ibm-cloud-vpn-proxy.ibm-vpn-proxy.svc:8080"}}
	]`)

	patchType := admission.PatchTypeJSONPatch
	admissionResult := admission.AdmissionResponse{
		UID:              review.Request.UID,
		Allowed:          true,
		Result:           nil,
		Patch:            patchJSON,
		PatchType:        &patchType,
		AuditAnnotations: nil,
		Warnings:         nil,
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
	http.HandleFunc("/", WebHookHandler)
	err := http.ListenAndServeTLS(":8443", "/var/run/secrets/webhook/tls.crt", "/var/run/secrets/webhook/tls.key", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
