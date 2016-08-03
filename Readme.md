---
####代码获取方法
github中执行`git clone _GIT_REPOSITRY_URL`获取代码，将会在本地目录生成一个appnet-agent目录。编译过程依赖于>该目录的目录结构，因此需要保持该目录的目录结构不变。

---
# 编译及测试方法
## 镜像编译方式
#### 1.编译环境准备
在代码根目录运行 `docker build -f appnet-agent/Dockerfile.build -t appnet-agent-build:latest .`
将会生成一个appnet-build:latest镜像。
#### 2.编译
在代码根目录下运行`docker run --rm -v $(pwd):/src/apnet-agent -v $(pwd)/dist:/src/dist  appnet-agent-build /src/appnet-agent/scripts/build`
执行该命令时,必须位于appnet/目录的父目录中。
编译完成后，将会在当前目录中创建一个dist目录，其中包含编译后的`appnet-agent`程序
