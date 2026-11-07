package classifier

import (
	"encoding/json"
	"strings"
)

// 分类体系：一套分类 + 两个维度（scope 决定家庭还是个人）
var CategoryNames = []string{
	"餐饮", "交通", "购物", "居家", "医疗", "教育",
	"娱乐", "人情", "通讯", "保险", "旅行", "其他",
}

const systemPrompt = `你是家庭记账本的分类引擎。给你一批消费记录，判断每笔的归属和分类。

【判断 scope】
- family：家庭共同开支。食材、日用品、水电燃气、房租物业、孩子相关、家庭聚餐、家居家电、家庭出行、车辆相关
- personal：个人开支。个人服饰、护肤化妆、个人爱好（游戏/健身）、个人社交、个人医美保健、单人娱乐
- transfer：资金往来。转给个人、收款码/经营码收款、红包往来，用途看不到具体商品或服务。这类**不算消费**，必须单独标记
- unknown：信息不足无法判断。宁可不判，也不要瞎猜

【判断 category】只能从下面选一个（这是该家庭实际拥有的分类）：
{{CATEGORIES}}

【参考示例】
{"merchant":"好想来零食乐园","item":"零食"} → {"scope":"family","category":"餐饮","confidence":0.85,"reason":"零食多为家庭共享"}
{"merchant":"屈臣氏","item":"面膜"} → {"scope":"personal","category":"购物","confidence":0.9,"reason":"个人护肤用品"}
{"merchant":"福建省高速公路集团","item":"高速通行费"} → {"scope":"family","category":"交通","confidence":0.95,"reason":"家庭用车通行费"}
{"merchant":"中国移动","item":"话费充值"} → {"scope":"personal","category":"通讯","confidence":0.8,"reason":"个人手机话费"}
{"merchant":"饭点餐厅","item":"餐饮"} → {"scope":"family","category":"餐饮","confidence":0.75,"reason":"餐厅消费，多为家庭用餐"}
{"merchant":"Steam","item":"游戏购买"} → {"scope":"personal","category":"娱乐","confidence":0.9,"reason":"个人游戏消费"}
{"merchant":"陌上花開","item":"收款方备注:二维码收款"} → {"scope":"transfer","category":"其他","confidence":0.8,"reason":"个人收款码，非商品消费"}
{"merchant":"。 (谢绍远)","item":"转账备注:微信转账"} → {"scope":"transfer","category":"其他","confidence":0.8,"reason":"个人转账，非消费"}
{"merchant":"中国移动","item":"话费充值"} → {"scope":"personal","category":"通讯","confidence":0.8,"reason":"个人话费"}

【输出要求】
严格返回 JSON 对象，形如 {"items":[{"id":"1","scope":"family","category":"餐饮","confidence":0.85,"reason":"不超过15字"}]}
- id 必须与输入一致
- scope 只能是 family / personal / unknown
- category 只能从上面的列表里选
- confidence 是 0-1 的小数
- 不要输出任何解释文字、不要用 markdown 代码块`

// Item 送给模型的一条记录
type Item struct {
	ID       string `json:"id"`
	Merchant string `json:"merchant"`
	Item     string `json:"item"`
	Amount   int64  `json:"amount_yuan"`
	Hour     int    `json:"hour"`
	Weekday  string `json:"weekday"`
}

// Decision 模型对一条记录的判断
type Decision struct {
	ID         string  `json:"id"`
	Scope      string  `json:"scope"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type response struct {
	Items []Decision `json:"items"`
}

func buildUserPayload(items []Item) (string, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// normalizeCategory 容错：模型可能返回「餐饮美食」这类近义词
func normalizeCategory(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "其他"
	}
	for _, name := range CategoryNames {
		if s == name {
			return name
		}
	}
	for _, name := range CategoryNames {
		if strings.Contains(s, name) {
			return name
		}
	}
	return "其他"
}

func normalizeScope(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "family", "家庭":
		return "family"
	case "personal", "个人":
		return "personal"
	case "transfer", "资金往来", "转账":
		return "transfer"
	default:
		return "unknown"
	}
}

func validate(d Decision) Decision {
	d.Scope = normalizeScope(d.Scope)
	d.Category = normalizeCategory(d.Category)
	if d.Confidence < 0 {
		d.Confidence = 0
	}
	if d.Confidence > 1 {
		d.Confidence = 1
	}
	if r := []rune(d.Reason); len(r) > 30 {
		d.Reason = string(r[:30])
	}
	return d
}

// systemPromptFor 把家庭实际拥有的分类填进提示词。
// 关键：模型只能选真实存在的分类，否则会给出「保险」这种不存在的大类，
// 落库时只能兜底到「其他」，白白浪费判断力。
func systemPromptFor(categories []string) string {
	list := strings.Join(categories, " ")
	return strings.Replace(systemPrompt, "{{CATEGORIES}}", list, 1)
}
