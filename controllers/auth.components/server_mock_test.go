package authcomponents

//
//import (
//	"encoding/json"
//	"net/http"
//
//	"github.com/gorilla/mux"
//	auth "github.com/numary/auth/pkg"
//)
//
//type authServerMock struct {
//	*mux.Router
//	clients          map[string]*auth.Client
//	generatedSecrets map[string]string
//}
//
//func newAuthServerMock() *authServerMock {
//
//	ret := &authServerMock{
//		Router:           mux.NewRouter(),
//		clients:          map[string]*auth.Client{},
//		generatedSecrets: map[string]string{},
//	}
//
//	writeJson := func(w http.ResponseWriter, v any) {
//		err := json.NewEncoder(w).Encode(v)
//		if err != nil {
//			panic(err)
//		}
//	}
//
//	ret.Router.NewRoute().Methods(http.MethodGet).Path("/clients/{clientId}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		client, ok := ret.clients[mux.Vars(r)["clientId"]]
//		if !ok {
//			w.WriteHeader(http.StatusNotFound)
//			return
//		}
//		writeJson(w, client)
//	})
//	ret.Router.NewRoute().Methods(http.MethodDelete).Path("/clients/{clientId}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		_, ok := ret.clients[mux.Vars(r)["clientId"]]
//		if !ok {
//			w.WriteHeader(http.StatusNotFound)
//			return
//		}
//		delete(ret.clients, mux.Vars(r)["clientId"])
//	})
//	ret.Router.NewRoute().Methods(http.MethodPost).Path("/clients").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//
//		client := &auth.Client{}
//		err := json.NewDecoder(r.Body).Decode(client)
//		if err != nil {
//			panic(err)
//		}
//
//		ret.clients[client.Id] = client
//
//		w.WriteHeader(http.StatusCreated)
//		writeJson(w, client)
//	})
//	ret.Router.NewRoute().Methods(http.MethodPatch).Path("/clients/{clientId}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//
//		client := &auth.Client{}
//		err := json.NewDecoder(r.Body).Decode(client)
//		if err != nil {
//			panic(err)
//		}
//
//		client.Id = mux.Vars(r)["clientId"]
//		ret.clients[client.Id] = client
//
//		w.WriteHeader(http.StatusOK)
//		writeJson(w, client)
//	})
//	ret.Router.NewRoute().Methods(http.MethodPost).Path("/clients/{clientId}/secrets").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//
//		client, ok := ret.clients[mux.Vars(r)["clientId"]]
//		if !ok {
//			w.WriteHeader(http.StatusNotFound)
//			return
//		}
//
//		type X struct {
//			Name string `json:"name"`
//		}
//
//		x := &X{}
//		err := json.NewDecoder(r.Body).Decode(x)
//		if err != nil {
//			panic(err)
//		}
//
//		secret, clear := client.GenerateNewSecret(x.Name)
//		ret.generatedSecrets[secret.ID] = clear
//
//		writeJson(w, secret)
//	})
//	ret.Router.NewRoute().Methods(http.MethodDelete).Path("/clients/{clientId}/secrets/{secretId}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//
//		client, ok := ret.clients[mux.Vars(r)["clientId"]]
//		if !ok {
//			w.WriteHeader(http.StatusNotFound)
//			return
//		}
//
//		if !client.DeleteSecret(mux.Vars(r)["secretId"]) {
//			w.WriteHeader(http.StatusNotFound)
//		}
//	})
//	return ret
//}
