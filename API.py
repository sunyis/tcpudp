import requests
import json

api_url = "http://127.0.0.1:7655/api/"
auth_code = "test"  # 验证码

# 添加端口映射
def add_mapping(listen_addr, forward_addr):
    headers = {"Authorization": auth_code}
    data = {"listenAddr": listen_addr, "forwardAddr": forward_addr, "mappingType" : "tcp"}
    response = requests.post(api_url + "add", json=data, headers=headers)
    print(response.status_code)
    print(response.text)

# 删除端口映射
def delete_mapping(listen_addr):
    headers = {"Authorization": auth_code}
    params = {"listenAddr": listen_addr, "mappingType": "tcpudp"}
    response = requests.delete(api_url + "delete", params=params, headers=headers)
    print(response.status_code)
    print(response.text)

# 查询端口映射
def query_mappings():
    headers = {"Authorization": auth_code}
    response = requests.get(api_url + "query", headers=headers)
    print(response.status_code)
    print(response.text)

# delete_mapping(":90")

add_mapping(":80", "192.168.8.1:80")
add_mapping(":81", "192.168.8.1:81")
add_mapping(":82", "192.168.8.1:82")
add_mapping(":83", "192.168.8.1:8")

query_mappings()
