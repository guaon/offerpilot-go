package server

import "net/http"

// handleLoginPage 提供独立的登录页面。
func (s *Server) handleLoginPage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(loginPageHTML))
}

const loginPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>登录 · OfferPilot</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f8fafc;color:#1e293b;min-height:100vh;display:flex;align-items:center;justify-content:center}
.card{background:#fff;border-radius:16px;border:1px solid #e2e8f0;padding:40px;width:100%;max-width:400px;box-shadow:0 4px 24px rgba(0,0,0,.06)}
.brand{display:flex;align-items:center;gap:12px;margin-bottom:28px;justify-content:center}
.brand-mark{width:44px;height:44px;border-radius:12px;background:linear-gradient(135deg,#0ea5e9,#06b6d4);color:#fff;display:flex;align-items:center;justify-content:center;font-size:20px;font-weight:700}
.brand-name{font-size:20px;font-weight:700}
h2{font-size:16px;font-weight:600;text-align:center;margin-bottom:24px;color:#475569}
.field{margin-bottom:16px}
.field label{display:block;font-size:13px;color:#64748b;margin-bottom:6px}
.field input{width:100%;padding:11px 14px;border:1px solid #e2e8f0;border-radius:10px;font-size:14px;outline:none;transition:border .15s}
.field input:focus{border-color:#0ea5e9;box-shadow:0 0 0 3px rgba(14,165,233,.1)}
.btn{width:100%;padding:12px;border:0;border-radius:10px;background:linear-gradient(135deg,#0ea5e9,#06b6d4);color:#fff;font-size:15px;font-weight:600;cursor:pointer;transition:opacity .15s}
.btn:hover{opacity:.9}
.btn:disabled{opacity:.5;cursor:not-allowed}
.err{display:none;background:#fef2f2;border:1px solid #fecaca;color:#b91c1c;padding:10px 12px;border-radius:8px;font-size:13px;margin-bottom:16px}
.err.show{display:block}
.hint{text-align:center;font-size:12px;color:#94a3b8;margin-top:20px}
.hint a{color:#0ea5e9;text-decoration:none}
</style>
</head>
<body>
<div class="card">
  <div class="brand"><div class="brand-mark">OP</div><div class="brand-name">OfferPilot</div></div>
  <h2>登录面试诊断 Agent</h2>
  <div class="err" id="errBox"></div>
  <div class="field">
    <label>用户名</label>
    <input type="text" id="username" placeholder="请输入用户名" autocomplete="username"/>
  </div>
  <div class="field">
    <label>密码</label>
    <input type="password" id="password" placeholder="请输入密码" autocomplete="current-password"/>
  </div>
  <button class="btn" id="loginBtn" onclick="doLogin()">登 录</button>
  <div class="hint">还没有账号？<a href="#" onclick="doRegister()">注册</a></div>
</div>
<script>
function showErr(msg){var e=document.getElementById("errBox");e.textContent=msg;e.classList.add("show")}
function clearErr(){document.getElementById("errBox").classList.remove("show")}
function submit(mode){
  clearErr();
  var u=document.getElementById("username").value.trim();
  var p=document.getElementById("password").value;
  if(!u||!p){showErr("请输入用户名和密码");return}
  var btn=document.getElementById("loginBtn");btn.disabled=true;
  var url=mode==="login"?"/api/login":"/api/register";
  fetch(url,{method:"POST",headers:{"Content-Type":"application/json"},credentials:"include",body:JSON.stringify({username:u,password:p})})
    .then(function(r){return r.json().then(function(d){return {ok:r.ok,d:d}})})
    .then(function(x){
      if(x.ok){location.href="/"}else{showErr(x.d.error||"操作失败");btn.disabled=false}
    })
    .catch(function(e){showErr("网络错误");btn.disabled=false});
}
function doLogin(){submit("login")}
function doRegister(){submit("register")}
document.getElementById("password").addEventListener("keydown",function(e){if(e.key==="Enter")doLogin()});
</script>
</body>
</html>`
