# API Layer

Axios封装。所有请求通过这里。

## FILES

| File | Purpose |
|------|---------|
| `request.js` | Axios实例、拦截器 |
| `user.js` | 用户相关API |
| `project.js` | 项目相关API |
| `pipeline.js` | 流水线相关API |

## REQUEST.JS PATTERN

```js
import axios from 'axios'

const request = axios.create({
    baseURL: '/api',
    timeout: 15000
})

// 请求拦截器
request.interceptors.request.use(...)

// 响应拦截器
request.interceptors.response.use(...)

export default request
```

## USAGE

```js
import { getProjectList, createProject } from '@/api/project'

const res = await getProjectList({ page: 1, page_size: 10 })
```
