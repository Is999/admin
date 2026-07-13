FROM apache/shardingsphere-proxy@sha256:a22d591d91922ac854e560774d6db2f468fc933f9666ae48f62b98b576d7413c AS common

# 基础镜像带有 Narayana 适配器但不含其运行库；本方案固定使用 Atomikos，必须把未选用适配器移出 classpath，避免 XA 启动时 SPI 加载失败。
RUN mkdir -p /opt/shardingsphere-proxy/disabled-lib \
    && mv /opt/shardingsphere-proxy/lib/shardingsphere-transaction-xa-narayana-5.5.3.jar \
        /opt/shardingsphere-proxy/disabled-lib/

# ShardingSphere 官方镜像不内置 MySQL Connector/J；固定版本和摘要避免构建时依赖漂移。
ADD --checksum=sha256:d77962877d010777cff997015da90ee689f0f4bb76848340e1488f2b83332af5 \
    https://repo1.maven.org/maven2/com/mysql/mysql-connector-j/8.4.0/mysql-connector-j-8.4.0.jar \
    /opt/shardingsphere-proxy/ext-lib/mysql-connector-j-8.4.0.jar

# management 仅在受控 DBA 网络的变更窗口短时运行，用于执行 DistSQL 和迁移。
FROM common AS management

# runtime 是应用流量唯一允许访问的镜像；移除 DistSQL 语法和处理插件，缩小应用账号修改规则和迁移状态的攻击面。
# ShardingSphere 核心仍处理 CREATE/DROP DATABASE，生产还必须由前置 SQL 防火墙拒绝应用连接上的 DDL、DCL 和 DistSQL。
FROM common AS runtime
RUN mkdir -p /opt/shardingsphere-proxy/disabled-distsql \
    && distsql_plugin_count=$(find /opt/shardingsphere-proxy/lib -maxdepth 1 -type f \
        \( -name '*distsql-handler-5.5.3.jar' -o -name '*distsql-parser-5.5.3.jar' -o -name '*distsql-statement-5.5.3.jar' \) \
        ! -name 'shardingsphere-parser-distsql-statement-5.5.3.jar' | wc -l) \
    && test "$distsql_plugin_count" -eq 48 \
    && find /opt/shardingsphere-proxy/lib -maxdepth 1 -type f \
        \( -name '*distsql-handler-5.5.3.jar' -o -name '*distsql-parser-5.5.3.jar' -o -name '*distsql-statement-5.5.3.jar' \) \
        ! -name 'shardingsphere-parser-distsql-statement-5.5.3.jar' \
        -exec mv -t /opt/shardingsphere-proxy/disabled-distsql {} +
