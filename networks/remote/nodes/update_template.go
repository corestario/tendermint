package main

import (
	"bytes"
	"os"
	"fmt"
	"os/exec"
	"encoding/json"
	"net/http"
)

/**
# get the IPs
ip0=`terraform output -json public_ips | jq '.value[0]'`
ip1=`terraform output -json public_ips | jq '.value[1]'`
ip2=`terraform output -json public_ips | jq '.value[2]'`
ip3=`terraform output -json public_ips | jq '.value[3]'`
ip4=`terraform output -json public_ips | jq '.value[4]'`

# to remove quotes
strip() {
  opt=$1
  temp="${opt%\"}"
  temp="${temp#\"}"
  echo $temp
}

ip0=$(strip $ip0)
ip1=$(strip $ip1)
ip2=$(strip $ip2)
ip3=$(strip $ip3)
ip4=$(strip $ip4)






# get each nodes ID then populate the ansible file
id0=`curl $ip0:26657/status | jq .result.node_info.id`
id1=`curl $ip1:26657/status | jq .result.node_info.id`
id2=`curl $ip2:26657/status | jq .result.node_info.id`
id3=`curl $ip3:26657/status | jq .result.node_info.id`
id4=`curl $ip4:26657/status | jq .result.node_info.id`

id0=$(strip $id0)
id1=$(strip $id1)
id2=$(strip $id2)
id3=$(strip $id3)
id4=$(strip $id4)

# remove file we'll re-write to with new info
old_ansible_file=$GOPATH/src/github.com/tendermint/tendermint/networks/remote/ansible/roles/install/templates/systemd.service.j2
rm $old_ansible_file

# need to populate the `--p2p.persistent_peers` flag
echo "[Unit]
Description={{service}}
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User={{service}}
Group={{service}}
PermissionsStartOnly=true
ExecStart=/usr/bin/tendermint node --proxy_app=kvstore --p2p.persistent_peers=$id0@$ip0:26656,$id1@$ip1:26656,$id2@$ip2:26656,$id3@$ip3:26656,$id4@$ip4:26656
ExecReload=/bin/kill -HUP \$MAINPID
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
" >> $old_ansible_file

 */

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
	cmd.Dir="/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/terraform/"


	cmdOutput := &bytes.Buffer{}
	cmd.Stdout = cmdOutput
	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Stderr.WriteString(err.Error())
	}


	ipList:=terraformOutput{}
	err=json.Unmarshal(cmdOutput.Bytes(), &ipList)
	if err!=nil {
		panic(err)
	}
	arr:=make(map[string]string, len(ipList.IPs))

	fmt.Println(ipList)

	for i:=range ipList.IPs {
		resp,err:=http.Get("http://"+ipList.IPs[i]+":26657/status")
		if err!=nil {
			fmt.Println(err)
			continue
		}

		r:=statusResponse{}
		err=json.NewDecoder(resp.Body).Decode(&r)
		if err!=nil {
			fmt.Println(err)
			continue
		}
		arr[ipList.IPs[i]]=r.Result.NodeInfo.ID
	}
	fmt.Println(arr)
	pp:="--p2p.persistent_peers="

	for k,v:=range arr {
		pp+=v+"@"+k+":26656,"
	}
	pp=pp[0:len(pp)-1]
fmt.Println(pp)

	f,err:=os.Create("/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/ansible/roles/install/templates/systemd.service.j2")

a:=`[Unit]
Description={{service}}
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User={{service}}
Group={{service}}
PermissionsStartOnly=true
ExecStart=/usr/bin/tendermint node --proxy_app=kvstore `+pp +
` 
ExecReload=/bin/kill -HUP $MAINPID
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
`

	f.WriteString(a)

}
