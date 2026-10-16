// SPDX-License-Identifier: AGPL-3.0-only

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeTBankPage — кабинет как SPA: разделы показываются скриптом, поля без
// стабильных id, сумма с маской, результат приходит после запроса к серверу.
const fakeTBankPage = `<!doctype html><meta charset=utf-8><title>Платежи — Т-Банк</title>
<style>.s{display:none}.s.on{display:block}</style>
<div id=menu class="s on"><h1>Платежи</h1>
  <div role=button data-go=phone>По номеру телефона</div><div role=button data-go=card>По номеру карты</div>
  <div role=button data-go=req>По реквизитам</div><div role=button data-go=fines>Штрафы</div></div>
<div id=phone class=s><h2>Перевод по номеру телефона</h2>
  <label>Номер телефона<input data-k=phone></label>
  <div role=button id=pick>Выбрать банк</div><ul id=banks class=s><li>Т-Банк</li><li>Сбербанк</li><li>ВТБ</li></ul>
  <input data-k=amount placeholder="Сумма"><textarea data-k=comment placeholder="Сообщение получателю"></textarea>
  <p class=who></p><p>Комиссия 0 ₽</p><button data-pay=sbp_phone>Перевести</button></div>
<div id=card class=s><h2>Перевод на карту</h2>
  <input data-k=card placeholder="Номер карты"><input data-k=amount placeholder="Сумма">
  <p>Без комиссии</p><button data-pay=card>Перевести</button></div>
<div id=req class=s><h2>Оплата по реквизитам</h2>
  <input data-k=payee placeholder="Наименование получателя"><input data-k=inn placeholder="ИНН"><input data-k=kpp placeholder="КПП">
  <input data-k=bik placeholder="БИК"><input data-k=account placeholder="Счёт получателя">
  <input data-k=purpose placeholder="Назначение платежа"><input data-k=uin placeholder="УИН (если есть)">
  <input data-k=amount placeholder="Сумма"><button data-pay=requisites>Оплатить</button></div>
<div id=fines class=s><h2>Штрафы и налоги</h2><input data-k=uin placeholder="УИН или номер постановления">
  <button id=find>Найти</button><div id=charge class=s><p>Штраф ГИБДД</p><p>К оплате 500 ₽</p><button data-pay=uin>Оплатить</button></div></div>
<div id=result class=s><h2 id=rt></h2></div>
<script>
const $ = s => document.querySelector(s), show = id => { document.querySelectorAll('.s').forEach(e => e.classList.remove('on')); $('#' + id).classList.add('on'); };
let section = 'menu', bank = 'Т-Банк';
document.querySelectorAll('[data-go]').forEach(e => e.onclick = () => { section = e.dataset.go; show(section); });
$('#pick').onclick = () => $('#banks').classList.add('on');
document.querySelectorAll('#banks li').forEach(e => e.onclick = () => { bank = e.textContent; $('#banks').classList.remove('on'); });
const amount = document.querySelectorAll('[data-k=amount]');
amount.forEach(a => a.addEventListener('input', () => { a.value = a.value.replace(/[^\d,]/g, ''); }));
$('[data-k=phone]').addEventListener('input', e => { const d = e.target.value.replace(/\D/g, '').slice(-10);
  e.target.value = '+7 ' + d; $('.who').textContent = d.length === 10 ? 'Получатель: Иван И.' : ''; });
$('#find').onclick = () => setTimeout(() => $('#charge').classList.add('on'), 200);
document.querySelectorAll('[data-pay]').forEach(b => b.onclick = async () => {
  const v = {kind: b.dataset.pay, bank};
  $('#' + section).querySelectorAll('[data-k]').forEach(i => v[i.dataset.k] = i.value);
  const r = await fetch('/api/op', {method: 'POST', body: JSON.stringify(v)}).then(r => r.json());
  show('result'); $('#rt').textContent = r.ok ? (section === 'phone' || section === 'card' ? 'Перевод выполнен' : 'Платёж выполнен') : 'Недостаточно средств';
});
</script>`

// fakeTBank — сайт банка: страница платежей, лента операций, приём операций.
type fakeTBank struct {
	mu  sync.Mutex
	ops []map[string]string
}

func (b *fakeTBank) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch r.URL.Path {
	case "/mybank/payments/":
		fmt.Fprint(w, fakeTBankPage)
	case "/events/feed/":
		b.mu.Lock()
		defer b.mu.Unlock()
		fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>События</title><h1>Сегодня</h1>")
		for i := len(b.ops) - 1; i >= 0; i-- {
			o := b.ops[i]
			who := map[string]string{"sbp_phone": "Иван И.", "card": "Перевод на карту", "requisites": o["payee"], "uin": "Штраф ГИБДД"}[o["kind"]]
			fmt.Fprintf(w, "<div><p>%s</p><p>−%s ₽</p></div>", who, o["amount"])
		}
	case "/api/op":
		var v map[string]string
		json.NewDecoder(r.Body).Decode(&v)
		if v["kind"] == "uin" {
			v["amount"] = "500"
		}
		b.mu.Lock()
		b.ops = append(b.ops, v)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	default:
		http.NotFound(w, r)
	}
}

// Коннектор tbank в настоящем daemon и настоящем Chrome (headless): www.tbank-online.com
// разрешается в фейковый сайт банка. Три операции — перевод по СБП по телефону,
// перевод по номеру карты, оплата счёта по УИН и по реквизитам — проходят и
// подтверждаются по ленте операций.
func TestTBankConnector(t *testing.T) {
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		if chrome, err = exec.LookPath("chromium"); err != nil {
			t.Skip("needs Chrome")
		}
	}
	bank := &fakeTBank{}
	srv := httptest.NewTLSServer(bank)
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]

	g := startGate(t)
	g.install("tbank")
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "chrome")
	os.WriteFile(wrapper, []byte("#!/bin/sh\nexec '"+chrome+"' --headless=new --disable-gpu --ignore-certificate-errors "+
		"'--host-resolver-rules=MAP www.tbank-online.com 127.0.0.1:"+port+"' \"$@\"\n"), 0o755)
	cfg := fmt.Sprintf(`agents:
  claude-ai: { profile: assistant }
profiles:
  assistant:
    connectors: [tbank]
    rules:
      - { id: tbank-test, action: "tbank.*", effect: allow }
connectors:
  tbank: {}
browser:
  engine: cdp
  chrome_path: %q
  profile_dir: %q
`, wrapper, filepath.Join(dir, "profile"))
	cfgFile := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgFile, []byte(cfg), 0o600)
	if r := g.run("", "", "policy", "set", cfgFile, "-m", "tbank e2e"); r.code != 0 {
		t.Fatalf("policy: %d %s", r.code, r.out)
	}
	token := g.run("", "", "agents", "token", "claude-ai").json(t)["token"].(string)

	for _, c := range [][]string{
		{"sbp_transfer_phone", "--phone", "+79001234567", "--amount", "10", "--bank", "Сбербанк", "--comment", "тест", "--expected_recipient", "Иван И."},
		{"card_transfer", "--card", "2200000000000004", "--amount", "15.5"},
		{"pay_bill", "--uin", "18810177230000000001", "--amount", "500"},
		{"pay_bill", "--payee", "ПАО Мосэнергосбыт", "--inn", "7736520080", "--bik", "044525225", "--account", "40702810638000000001",
			"--purpose", "ЛС 12345 октябрь", "--amount", "1234.5"},
	} {
		r := g.run(token, "", append([]string{"tbank"}, c...)...)
		res := r.json(t)
		if r.code != 0 || res["status"] != "ok" || res["verified"] != true {
			t.Fatalf("tbank %s: %d %s", c[0], r.code, r.out)
		}
	}
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if len(bank.ops) != 4 {
		t.Fatalf("bank got %d operations: %v", len(bank.ops), bank.ops)
	}
	want := []map[string]string{
		{"kind": "sbp_phone", "phone": "+7 9001234567", "amount": "10", "bank": "Сбербанк", "comment": "тест"},
		{"kind": "card", "card": "2200000000000004", "amount": "15,50"},
		{"kind": "uin", "uin": "18810177230000000001"},
		{"kind": "requisites", "inn": "7736520080", "bik": "044525225", "account": "40702810638000000001", "amount": "1234,50", "payee": "ПАО Мосэнергосбыт"},
	}
	for i, w := range want {
		for k, v := range w {
			if bank.ops[i][k] != v {
				t.Errorf("op %d %s: %q, want %q", i, k, bank.ops[i][k], v)
			}
		}
	}
}
