package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type TcpPortMapping struct {
	ListenAddr  string
	ForwardAddr string
	Listener    net.Listener
}

type UdpPortMapping struct {
	ListenAddr  string
	ForwardAddr string
	Listener    net.PacketConn
}

type Mapping struct {
	SourcePort  int    `yaml:"source_port"`
	TargetIP    string `yaml:"target_ip"`
	TargetPort  int    `yaml:"target_port"`
	MappingType string `yaml:"mapping_type"`
}

type Config struct {
	Mappings []Mapping `yaml:"mappings"`
}

var (
	mappingsTcp = &sync.Map{}
	mappingsUdp = &sync.Map{}
	config      Config

	version  = "1.1.0"
	apiPort  string // API监听端口
	authCode string // API访问授权码
)

func addTcpMapping(listenAddr, forwardAddr string) error {

	ln, err := net.Listen("tcp", listenAddr)

	if err != nil {
		return err
	}

	mapping := &TcpPortMapping{
		ListenAddr:  listenAddr,
		ForwardAddr: forwardAddr,
		Listener:    ln,
	}

	log.Printf("正在监听 TCP %s 并转发至 %s\n", mapping.ListenAddr, mapping.ForwardAddr)

	mappingsTcp.Store(listenAddr, mapping)
	go handleTcpConnections(mapping)
	return nil
}

func handleTcpConnections(mapping *TcpPortMapping) {
	for {
		conn, err := mapping.Listener.Accept()

		if err != nil {
			log.Printf("接受连接失败 %s: %s", mapping.ListenAddr, err)
			return
		}
		go handleTcpRequest(conn, mapping.ForwardAddr)
	}
}

func handleTcpRequest(src net.Conn, forwardAddr string) {

	localAddr := src.LocalAddr().String()
	if localAddr == forwardAddr {
		log.Printf("源和目标地址相同，关闭连接: %s", localAddr)
		src.Close()
		return
	}

	_, err := net.ResolveTCPAddr("tcp", forwardAddr)
	if err != nil {
		log.Printf("无法解析转发地址: %s", err)
		src.Close()
		return
	}

	log.Printf("连接建立: %s -> %s\n", src.RemoteAddr(), forwardAddr)
	dest, err := net.Dial("tcp", forwardAddr)
	if err != nil {
		log.Printf("连接转发地址失败: %s", err)
		src.Close()
		return
	}

	timeout := 30 * time.Second
	TcpPipe(src, dest, timeout)
}

func TcpPipe(src, dest net.Conn, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go copyAndHandleTcp(ctx, src, dest, &wg)
	go copyAndHandleTcp(ctx, dest, src, &wg)
	wg.Wait()
}

func copyAndHandleTcp(ctx context.Context, src, dst net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	select {
	case <-ctx.Done():
		log.Println("超时")
		return
	default:
		if _, err := io.Copy(dst, src); err != nil {
			log.Printf("管道错误: %s", err)
			return
		}
	}
}

func deleteTcpMapping(listenAddr string) error {
	value, ok := mappingsTcp.Load(listenAddr)
	if !ok {
		return fmt.Errorf("未找到映射")
	}
	mapping := value.(*TcpPortMapping)
	mapping.Listener.Close()
	mappingsTcp.Delete(listenAddr)
	return nil
}

func UdpPipe(src, dest net.PacketConn, srcAddr net.Addr, destAddr net.Addr, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go copyAndHandleUdp(ctx, src, dest, srcAddr, &wg)
	go copyAndHandleUdp(ctx, dest, src, destAddr, &wg)
	wg.Wait()
}

func copyAndHandleUdp(ctx context.Context, src, dst net.PacketConn, dstAddr net.Addr, wg *sync.WaitGroup) {
	defer wg.Done()
	buffer := make([]byte, 2048) // 根据需要调整缓冲区大小

	for {
		select {
		case <-ctx.Done():
			log.Println("超时")
			return
		default:
			n, _, err := src.ReadFrom(buffer)
			if err != nil {
				log.Printf("读取错误: %s", err)
				return
			}

			if _, err := dst.WriteTo(buffer[:n], dstAddr); err != nil {
				log.Printf("写入错误: %s", err)
				return
			}
		}
	}
}

func handleUdpRequest(src net.PacketConn, forwardAddr string, srcAddr net.Addr) {
	localAddr := src.LocalAddr().String()
	if localAddr == forwardAddr {
		log.Printf("源和目标地址相同，关闭连接: %s", localAddr)
		return
	}

	_, err := net.ResolveUDPAddr("udp", forwardAddr)
	if err != nil {
		log.Printf("无法解析转发地址: %s", err)
		return
	}

	log.Printf("连接建立: %s -> %s\n", srcAddr, forwardAddr)

	// 设置一个超时时间
	timeout := 30 * time.Second
	_, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dest, err := net.Dial("udp", forwardAddr)
	if err != nil {
		log.Printf("连接转发地址失败: %s", err)
		return
	}
	defer dest.Close()

	UdpPipe(src, dest.(net.PacketConn), srcAddr, dest.RemoteAddr(), timeout)
}

func handleUdpConnections(mapping *UdpPortMapping) {
	buffer := make([]byte, 2048) // 根据需要调整缓冲区大小

	for {
		_, srcAddr, err := mapping.Listener.ReadFrom(buffer)
		if err != nil {
			log.Printf("接受数据包失败 %s: %s", mapping.ListenAddr, err)
			return
		}

		go handleUdpRequest(mapping.Listener, mapping.ForwardAddr, srcAddr)
	}
}

func addUdpMapping(listenAddr, forwardAddr string) error {
	// 使用 net.ListenPacket 创建 UDP 监听器
	ln, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return err
	}

	mapping := &UdpPortMapping{
		ListenAddr:  listenAddr,
		ForwardAddr: forwardAddr,
		Listener:    ln, // 这里的 ln 是 net.PacketConn 类型
	}

	log.Printf("正在监听 UDP: %s 并转发至 %s\n", mapping.ListenAddr, mapping.ForwardAddr)

	mappingsUdp.Store(listenAddr, mapping)
	go handleUdpConnections(mapping)
	return nil
}

func deleteUdpMapping(listenAddr string) error {
	value, ok := mappingsUdp.Load(listenAddr)
	if !ok {
		return fmt.Errorf("未找到映射")
	}
	mapping := value.(*UdpPortMapping)
	mapping.Listener.Close()
	mappingsUdp.Delete(listenAddr)
	return nil
}

func apiAddMapping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "无效的请求方法", http.StatusMethodNotAllowed)
		return
	}
	if err := validateAuthCode(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var data map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	if err := decoder.Decode(&data); err != nil {
		http.Error(w, "无效的 JSON", http.StatusBadRequest)
		return
	}

	var listenAddr, forwardAddr, mappingType string

	if _listenAddr, ok := data["listenAddr"].(string); ok {
		listenAddr = _listenAddr
	} else {
		http.Error(w, "缺少listenAddr参数", http.StatusBadRequest)
	}

	if _forwardAddr, ok := data["forwardAddr"].(string); ok {
		forwardAddr = _forwardAddr
	} else {
		http.Error(w, "缺少forwardAddr参数", http.StatusBadRequest)
	}

	if _mappingType, ok := data["mappingType"].(string); ok {
		mappingType = _mappingType
	} else {
		http.Error(w, "缺少mappingType参数", http.StatusBadRequest)
	}

	if mappingType == "tcp" {
		if err := addTcpMapping(listenAddr, forwardAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if mappingType == "udp" {
		if err := addUdpMapping(listenAddr, forwardAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if mappingType == "udptcp" || mappingType == "tcpudp" {
		if err := addUdpMapping(listenAddr, forwardAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := addTcpMapping(listenAddr, forwardAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if temp, ok := data["temp"].(bool); ok {
		if temp {
			w.WriteHeader(http.StatusOK)
			fmt.Print("不保存到端口映射配置文件")
			return
		}
	}

	var SourcePort, TargetPort int
	fmt.Sscan(strings.Split(listenAddr, ":")[1], &SourcePort)
	fmt.Sscan(strings.Split(forwardAddr, ":")[1], &TargetPort)

	newMapping := Mapping{
		SourcePort:  SourcePort,
		TargetIP:    strings.Split(forwardAddr, ":")[0],
		TargetPort:  TargetPort,
		MappingType: mappingType,
	}

	if mappingLen := len(config.Mappings); mappingLen == 0 {
		config.Mappings = []Mapping{newMapping} // 初始化切片并赋值
	} else {
		config.Mappings = append(config.Mappings, newMapping)
	}

	writeConfig("config.yml")

	w.WriteHeader(http.StatusOK)
}

func removeMappingBySourcePort(mappings []Mapping, sourcePort int, mappingType string) []Mapping {
	var updatedMappings []Mapping
	for _, mapping := range mappings {
		if mapping.SourcePort != sourcePort {
			updatedMappings = append(updatedMappings, mapping)
		}
		if mapping.SourcePort == sourcePort && (mapping.MappingType == "tcpudp" || mapping.MappingType == "udptcp") {
			if mappingType == "tcp" {
				mapping.MappingType = "udp"
				updatedMappings = append(updatedMappings, mapping)
			}
			if mappingType == "udp" {
				mapping.MappingType = "tcp"
				updatedMappings = append(updatedMappings, mapping)
			}
		}
	}
	return updatedMappings
}

func apiDeleteMapping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "无效的请求方法", http.StatusMethodNotAllowed)
		return
	}
	if err := validateAuthCode(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	listenAddr := r.URL.Query().Get("listenAddr")
	if listenAddr == "" {
		http.Error(w, "缺少listenAddr参数", http.StatusBadRequest)
		return
	}

	mappingType := r.URL.Query().Get("mappingType")
	if mappingType == "" {
		http.Error(w, "缺少mappingType参数", http.StatusBadRequest)
		return
	}

	if mappingType == "tcp" {

		if err := deleteTcpMapping(listenAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	}

	if mappingType == "udp" {
		if err := deleteUdpMapping(listenAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if mappingType == "tcpudp" || mappingType == "udptcp" {

		if err := deleteTcpMapping(listenAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := deleteUdpMapping(listenAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var SourcePort int
	fmt.Sscan(strings.Split(listenAddr, ":")[1], &SourcePort)

	config.Mappings = removeMappingBySourcePort(config.Mappings, SourcePort, mappingType)
	writeConfig("config.yml")

	w.WriteHeader(http.StatusOK)
}

func apiQueryMappings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "无效的请求方法", http.StatusMethodNotAllowed)
		return
	}
	if err := validateAuthCode(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	type MappingInfo struct {
		ListenAddr  string `json:"listen_addr"`
		ForwardAddr string `json:"forward_addr"`
		MappingType string `json:"mapping_type"`
	}

	var mappingsInfo []MappingInfo
	mappingsTcp.Range(func(key, value interface{}) bool {
		mapping := value.(*TcpPortMapping)
		mappingsInfo = append(mappingsInfo, MappingInfo{
			ListenAddr:  mapping.ListenAddr,
			ForwardAddr: mapping.ForwardAddr,
			MappingType: "tcp",
		})
		return true
	})

	mappingsUdp.Range(func(key, value interface{}) bool {
		mapping := value.(*UdpPortMapping)
		mappingsInfo = append(mappingsInfo, MappingInfo{
			ListenAddr:  mapping.ListenAddr,
			ForwardAddr: mapping.ForwardAddr,
			MappingType: "udp",
		})
		return true
	})

	jsonResponse, err := json.Marshal(mappingsInfo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeConfig("config.yml"); err != nil {
		fmt.Print(err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonResponse)
}

func validateAuthCode(r *http.Request) error {
	code := r.Header.Get("Authorization")
	if code != authCode {
		return fmt.Errorf("未授权的访问")
	}
	return nil
}

func writeConfig(filename string) error {
	// 将配置序列化为 YAML 格式

	data, err := yaml.Marshal(&config)
	if err != nil {
		fmt.Println("数据错误")
		fmt.Println(err)
		return err
	}

	// 写入文件
	err = os.WriteFile(filename, data, 0777)
	if err != nil {
		fmt.Println("文件写入错误")
		fmt.Println(err)
		return err
	}

	return nil
}

func parseConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil
}

func parseArgs() {

	if !fileExists("config.yml") {
		file, err := os.Create("config.yml")
		if err != nil {
			fmt.Println("创建配置文件时出错:", err)
			return
		}
		defer file.Close() // 确保在函数结束时关闭文件
	}

	config, err := parseConfig("config.yml")
	if err != nil {
		log.Fatalf("无法打开配置文件: %v", err)
	}

	flag.StringVar(&apiPort, "p", "7655", "API服务监听端口")
	flag.StringVar(&authCode, "code", "", "API访问授权码 (必填)")
	versionFlag := flag.Bool("v", false, "打印版本并退出")

	flag.Parse()

	if *versionFlag {
		fmt.Println("版本:", version)
		fmt.Println("iris-n2n-launcher-3 组件")
		fmt.Println("花之链环： @5656565566")
		os.Exit(0)
	}

	if authCode == "" {
		flag.Usage()
		log.Fatalf("未提供API访问授权码")
	}

	for _, m := range config.Mappings {

		var listenAddr string = fmt.Sprintf(":%d", m.SourcePort)
		var forwardAddr string = fmt.Sprintf("%s:%d", m.TargetIP, m.TargetPort)

		if m.MappingType == "tcp" {
			addTcpMapping(listenAddr, forwardAddr)
		}
		if m.MappingType == "udp" {
			addUdpMapping(listenAddr, forwardAddr)
		}
		if m.MappingType == "tcpudp" || m.MappingType == "udptcp" || m.MappingType == "" {
			addTcpMapping(listenAddr, forwardAddr)
			addUdpMapping(listenAddr, forwardAddr)
		}
	}
}

func main() {
	parseArgs()

	http.HandleFunc("/api/add", apiAddMapping)
	http.HandleFunc("/api/delete", apiDeleteMapping)
	http.HandleFunc("/api/query", apiQueryMappings)
	log.Printf("API 服务正在监听 %s 端口", apiPort)
	log.Fatal(http.ListenAndServe("127.0.0.1:"+apiPort, nil))
}
