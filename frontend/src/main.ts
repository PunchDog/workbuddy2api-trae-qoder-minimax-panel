/*
 * main.ts —— 应用入口。
 *
 * T01 用 `import TDesign from 'tdesign-vue-next'` 全量注册，JS 产物 1.4MB；
 * T03 改为按需引入组件 + 按组件 CSS，配合 tree-shaking 收敛体积。
 * 全局令牌来自 styles/tokens.css，暗色模式由 useTheme 写 html[theme-mode]。
 */
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  Button,
  ConfigProvider,
  Dialog,
  Input,
  Select,
  Option,
  Table,
  Tag,
  Loading,
  Tooltip,
  Tabs,
  TabPanel,
  Textarea,
  InputNumber,
  Form,
  FormItem,
  Switch,
  Progress,
  Checkbox,
  Collapse,
  CollapsePanel,
  Divider,
  Alert,
} from 'tdesign-vue-next'

// TDesign 全局主题变量（:root 的 --td-*）：按需引入组件 CSS 不会自动带入。
// 缺失时 Tabs 的 .t-tabs__nav-wrap 靠 font-size:0 + --td-font-body-medium
// 恢复标签字号，变量为空会把标签宽高压成 0（实测 0×0，只剩首个 tab 内容可见）。
import 'tdesign-vue-next/es/style/index.css'

// 只加载实际使用的组件样式，避免整包 CSS 进入产物。
import 'tdesign-vue-next/es/button/style/index.css'
import 'tdesign-vue-next/es/dialog/style/index.css'
import 'tdesign-vue-next/es/input/style/index.css'
import 'tdesign-vue-next/es/select/style/index.css'
import 'tdesign-vue-next/es/table/style/index.css'
import 'tdesign-vue-next/es/tag/style/index.css'
import 'tdesign-vue-next/es/loading/style/index.css'
import 'tdesign-vue-next/es/tooltip/style/index.css'
import 'tdesign-vue-next/es/tabs/style/index.css'
import 'tdesign-vue-next/es/textarea/style/index.css'
import 'tdesign-vue-next/es/input-number/style/index.css'
import 'tdesign-vue-next/es/form/style/index.css'
import 'tdesign-vue-next/es/switch/style/index.css'
import 'tdesign-vue-next/es/progress/style/index.css'
import 'tdesign-vue-next/es/checkbox/style/index.css'
import 'tdesign-vue-next/es/collapse/style/index.css'
import 'tdesign-vue-next/es/divider/style/index.css'
import 'tdesign-vue-next/es/alert/style/index.css'

import '@/styles/app.css'
import App from './App.vue'

const app = createApp(App)
app.use(createPinia())

// ConfigProvider 在 App.vue 内包裹布局，这里只做组件注册。
app.use(Button)
app.use(ConfigProvider)
app.use(Dialog)
app.use(Input)
app.use(Select)
app.use(Option)
app.use(Table)
app.use(Tag)
app.use(Loading)
app.use(Tooltip)
app.use(Tabs)
app.use(TabPanel)
app.use(Textarea)
app.use(InputNumber)
app.use(Form)
app.use(FormItem)
app.use(Switch)
app.use(Progress)
app.use(Checkbox)
app.use(Collapse)
app.use(CollapsePanel)
app.use(Divider)
app.use(Alert)

app.mount('#app')
