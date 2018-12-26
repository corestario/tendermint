package main

import (
	"bytes"
	"os"
	"fmt"
	"os/exec"
	"encoding/json"
	"net/http"
)

type terraformOutput struct {
	IPs []string `json:"value"`
}
type statusResponse struct {
	Result Result `json:"result"`
}
type Result struct {
	NodeInfo nodeInfo `json:"node_info"`
}
type nodeInfo struct {
	ID string `json:"id"`
}

func main() {
	cmd := exec.Command("terraform", "output", "-json", "public_ips")
	cmd.Dir = "/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/terraform/"

	cmdOutput := &bytes.Buffer{}
	cmd.Stdout = cmdOutput
	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Stderr.WriteString(err.Error())
	}

	ipList := terraformOutput{}
	err = json.Unmarshal(cmdOutput.Bytes(), &ipList)
	if err != nil {
		panic(err)
	}
	arr := make(map[string]string, len(ipList.IPs))

	fmt.Println("IPs", ipList)

	for i := range ipList.IPs {
		resp, err := http.Get("http://" + ipList.IPs[i] + ":26657/status")
		if err != nil {
			fmt.Println(err)
			continue
		}

		r := statusResponse{}
		err = json.NewDecoder(resp.Body).Decode(&r)
		if err != nil {
			fmt.Println(err)
			continue
		}
		arr[ipList.IPs[i]] = r.Result.NodeInfo.ID
	}

	pp := "--p2p.persistent_peers="

	for k, v := range arr {
		pp += v + "@" + k + ":26656,"
	}
	pp = pp[0 : len(pp)-1]
	fmt.Println("Persistent peers:", pp)

	f, err := os.Create("/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/ansible/roles/install/templates/systemd.service.j2")

	a := `[Unit]
Description={{service}}
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User={{service}}
Group={{service}}
PermissionsStartOnly=true
ExecStart=/usr/bin/tendermint node --proxy_app=kvstore ` + pp +
		` 
ExecReload=/bin/kill -HUP $MAINPID
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
`

	f.WriteString(a)

}
