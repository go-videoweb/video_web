# API 接口测试：未覆盖接口统计

说明：



- `已弃用`：条目中有明确的 `deprecated` 或 `departed` 弃用说明，已同步加入 `tests/api/api_deprecated_routes.json`。

- `需要为其添加测试用例`：条目中已有请求或响应示例，或备注明确说明后续要实现测试。

- `不确定`：条目缺少请求/响应/备注，或备注中说明暂不处理、疑似弃用、临时接口等，需要进一步确认。

统计：



|状态|数量|
|---|---|
|已弃用|11|
|需要为其添加测试用例|32|
|不确定|9|
|合计|52|



> 注：标记为橙色的路由均由@张恩阳补齐接口测试用例
> 
> 

|路由路径|API 方法|状态|备注|
|---|---|---|---|
|`/api/v2/file/result/all/get`|`POST`|需要为其添加测试用例||
|`/api/v2/model/check`|`POST`|需要为其添加测试用例||
|`/api/v2/model/userspace/model/getall`|`POST`|需要为其添加测试用例||
|`/api/v2/simulation/record/get/finished`|`GET`|需要为其添加测试用例||
|`/api/v2/simulation/result/play`|`POST`|不确定，暂不添加测试用例||
|~~`/api/v2/user/userspace/get`~~|~~`GET`~~|~~已弃用~~||
|`/api/v2/user/userspace/usage`|`GET`|需要为其添加测试用例||
|`/api/v3/model/diagram/shapes/update`|`POST`|需要为其添加测试用例||
|`/api/v3/model/instantiate/code/get`|`GET`|不确定，暂不添加测试用例|未开发完成，暂时不用添加|
|~~`/api/v3/model/state/get`~~|~~`GET`~~|~~已弃用~~||
|`/api/v3/model/translate`|`POST`|需要为其添加测试用例||
|`/api/v3/simulation/result`|`POST`|需要为其添加测试用例|未开发完成，暂时不用添加|
|`/api/v3/simulation/result/singular`|`POST`|需要为其添加测试用例|未开发完成，暂时不用添加|
|`/file/fmu/export`|`POST`|需要为其添加测试用例||
|~~`/file/package`~~|`POST`|~~已弃用~~||
|`/model/class/move-to-package`|`POST`|需要为其添加测试用例||
|~~`/model/component/batch/update`~~|~~`POST`~~|~~已弃用~~||
|~~`/model/icon/graphics/new`~~|~~`POST`~~|~~已弃用~~||
|`/model/package/get/all`|`GET`|需要为其添加测试用例||
|`/model/translate/log/parse`|`POST`|需要为其添加测试用例||
|`/pipenet/info/copy`|`POST`|需要为其添加测试用例||
|`/pipenet/info/delete`|`POST`|需要为其添加测试用例||
|`/pipenet/info/download`|`POST`|需要为其添加测试用例||
|`/pipenet/info/edit`|`POST`|需要为其添加测试用例||
|`/pipenet/info/get`|`GET`|不确定|不用添加测试用例|
|`/pipenet/info/list/get`|`GET`|需要为其添加测试用例||
|`/pipenet/info/update`|`POST`|不确定|不用添加测试用例|
|`/pipenet/info/upload`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/copy`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/delete`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/details/edit`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/details/get`|`GET`|需要为其添加测试用例||
|`/pipenet/mapping_config/download`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/edit`|`POST`|需要为其添加测试用例||
|`/pipenet/mapping_config/list/get`|`GET`|需要为其添加测试用例||
|`/pipenet/mapping_config/upload`|`POST`|需要为其添加测试用例||
|`/pipenet/model/create`|`POST`|需要为其添加测试用例||
|`/pipenet/model/instance_mapping/get`|`GET`|需要为其添加测试用例||
|`/pipenet/model/instance_mapping/log/get`|`GET`|不确定|不用添加测试用例|
|~~`/simulation/animation/visual/get`~~|`GET`|~~已弃用~~||
|`/simulation/experiment/default/set`|`POST`|需要为其添加测试用例||
|~~`/simulation/experiment/edit`~~|~~`POST`~~|~~已弃用~~||
|~~`/simulation/experiment/parameters`~~|~~`GET`~~|~~已弃用~~||
|`/simulation/realtime-fmu/stream`|`GET`|需要为其添加测试用例||
|~~`/simulation/record/delete`~~|`GET`|~~已弃用~~||
|~~`/simulation/record/rename`~~|~~`POST`~~|~~已弃用~~||
|~~`/simulation/record/tree`~~|~~`GET`~~|~~已弃用~~||
|~~`/simulation/result/variable/unitconvert`~~|~~`GET`~~|~~已弃用~~||
|~~`/user/service/start`~~|~~`POST`~~|~~已弃用~~||
|~~`/user/service/stop`~~|~~`POST`~~|~~已弃用~~||
|`/user/userspace/task-log/clear`|`POST`|需要为其添加测试用例||
|`/user/userspace/task/terminate/all`|`POST`|需要为其添加测试用例||



