import os
import platform
import subprocess

# 定义支持的平台和架构
platforms = [
    ("windows", "amd64"),
    ("windows", "386"),
    ("linux", "amd64"),
    ("linux", "386"),
    ("linux", "arm"),
    ("darwin", "amd64"),  # macOS
    ("darwin", "arm64"),  # macOS ARM
    #("android13", "arm"),   # 安卓13 ARM
]

current_os = platform.system().lower()
current_arch = platform.machine()

android_sdk_version = {
    "android14": "34",
    "android13": "33",
    "android12s2": "32",
    "android12": "31",
    "android11": "30",
    "android10": "29",
    "android9": "28",
    "android8.1": "27",
    "android8": "26",
    "android7.1": "25",
    "android7.0": "24",
    "android6.0": "23",
    "android5.1": "22",
    "android5.0": "21",
    "android4.4w": "20",
    "android4.4": "19",
    "android4.3": "18",
    "android4.2.2": "17",
    "android4.2": "17",
    "android4.1.1": "16",
    "android4.1": "16",
    "android4.0.4": "15",
    "android4.0.3": "15",
    "android4.0.2": "14",
    "android4.0.1": "14",
    "android4.0": "14",
    "android3.2": "13",
    "android3.1": "12",
    "android3.0": "11",
    "android2.3.4": "10",
    "android2.3.3": "10",
    "android2.3.2": "9",
    "android2.3.1": "9",
    "android2.3": "9",
    "android2.2.0": "8",
    "android2.1.0": "7",
    "android2.0.1": "6",
    "android2.0": "5",
    "android1.6": "4",
    "android1.5": "3",
    "android1.1": "2",
    "android1.0": "1"
}

# Go文件的名称
go_file = "main.go"

# 创建输出目录
output_dir = "build"
os.makedirs(output_dir, exist_ok=True)

# 安卓 NDK 路径
android_ndk_path = "C:\\project\\sdk_runtime\\android-ndk-r26d"

if not os.path.exists(android_ndk_path):
    print("安卓 ndk 不存在 无法编译安卓版本")

for os_name, arch in platforms:
    env = os.environ.copy()
    env["GOOS"] = os_name
    env["GOARCH"] = arch
    
    if "android" in os_name and arch == "arm":
        
        sdk_version = android_sdk_version.get(os_name)
        
        env["GOOS"] = "android"
        
        env["CGO_ENABLED"] = "1"
        env["CC"] = os.path.join(android_ndk_path, "toolchains", "llvm", "prebuilt", "windows-x86_64", "bin", f"armv7a-linux-androideabi{sdk_version}-clang.cmd")
        env["CXX"] = os.path.join(android_ndk_path, "toolchains", "llvm", "prebuilt", "windows-x86_64", "bin", f"armv7a-linux-androideabi{sdk_version}-clang++.cmd")
    else:
        env["CGO_ENABLED"] = "0"

    output_name = f"{os_name}_{arch}_{os.path.splitext(go_file)[0]}"
    
    if os_name == "windows":
        output_name += ".exe"
    
    output_path = os.path.join(output_dir, output_name)
    
    cmd = ["go", "build", "-ldflags", "-s -w", "-o", output_path, go_file]
    result = subprocess.run(cmd, env=env, capture_output=True, text=True)
    
    if result.returncode == 0:
        print(f"成功编译了 {output_name}")
    else:
        print(f"{output_name} 编译失败了")
        print(result.stdout)
        print(result.stderr)