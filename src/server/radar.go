package server

import "net/http"

// handleRadar serves a self-contained radar chart page that visualizes
// dimension scores from GET /api/diagnosis.
func (s *Server) handleRadar(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(radarPageHTML))
}

const radarPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>OfferPilot ability radar</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.7/dist/chart.umd.min.js">
</script>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f8fafc;color:#1e293b}
.topbar{background:#fff;border-bottom:1px solid #e2e8f0;padding:0 24px;height:56px;display:flex;align-items:center;justify-content:space-between}
.brand{display:flex;align-items:center;gap:10px;font-weight:700;font-size:16px}
.brand-mark{width:32px;height:32px;border-radius:8px;background:linear-gradient(135deg,#0ea5e9,#06b6d4);color:#fff;display:flex;align-items:center;justify-content:center;font-size:14px}
.wrap{max-width:960px;margin:0 auto;padding:32px 24px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:24px}
@media(max-width:720px){.grid{grid-template-columns:1fr}}
.card{background:#fff;border-radius:16px;border:1px solid #e2e8f0;padding:24px;box-shadow:0 1px 3px rgba(0,0,0,.04)}
.card h2{font-size:15px;font-weight:600;margin-bottom:16px}
.empty{text-align:center;color:#94a3b8;padding:40px 0;font-size:14px}
.loading{text-align:center;padding:40px;color:#64748b}
.btn-row{display:flex;gap:8px;margin-top:16px}
.btn{padding:8px 18px;border-radius:10px;font-size:13px;cursor:pointer;border:1px solid #e2e8f0;background:#fff;color:#334155;font-weight:500;transition:all .15s;text-decoration:none}
.btn:hover{background:#f1f5f9}
.note{font-size:12px;color:#94a3b8;margin-top:8px}
.rec-item{padding:10px 12px;border-bottom:1px solid #f1f5f9;font-size:13px;display:flex;justify-content:space-between;cursor:pointer;transition:background .12s}
.rec-item:hover{background:#f8fafc}
.rec-item .q{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin-right:12px}
.rec-item .s{font-weight:600;shrink:0}
/* modal */
.modal-mask{position:fixed;inset:0;background:rgba(15,23,42,.45);display:none;align-items:center;justify-content:center;z-index:50;padding:24px}
.modal-mask.open{display:flex}
.modal{background:#fff;border-radius:16px;max-width:720px;width:100%;max-height:80vh;display:flex;flex-direction:column;overflow:hidden;box-shadow:0 20px 60px rgba(0,0,0,.2)}
.modal-head{padding:16px 20px;border-bottom:1px solid #e2e8f0;display:flex;justify-content:space-between;align-items:center}
.modal-head h3{font-size:14px;font-weight:600}
.modal-close{border:0;background:#f1f5f9;width:28px;height:28px;border-radius:8px;cursor:pointer;font-size:14px;color:#64748b}
.modal-close:hover{background:#e2e8f0}
.modal-body{overflow-y:auto;padding:16px 20px}
.modal-loading{text-align:center;color:#94a3b8;padding:32px 0}
.chat-msg{margin-bottom:12px}
.chat-msg .who{font-size:11px;font-weight:600;margin-bottom:3px}
.chat-msg.user .who{color:#0ea5e9}
.chat-msg.assistant .who{color:#0f766e}
.chat-msg .bubble{padding:10px 14px;border-radius:12px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-word}
.chat-msg.user .bubble{background:#e0f2fe;color:#0c4a6e}
.chat-msg.assistant .bubble{background:#f0fdfa;color:#134e4a}
.chat-msg.tool .bubble{background:#fef3c7;color:#92400e;font-size:12px}
</style>
</head>
<body>
<div class="topbar">
  <div class="brand"><div class="brand-mark">OP</div>OfferPilot ability radar</div>
  <a class="btn" href="/">back to chat</a>
</div>
<div class="wrap">
  <div class="grid">
    <div class="card">
      <h2>ability radar</h2>
      <div style="max-width:420px;margin:0 auto"><canvas id="radarChart"></canvas></div>
    </div>
    <div class="card">
      <h2>stats</h2>
      <div id="statsBox"><div class="loading">loading...</div></div>
    </div>
    <div class="card">
      <h2>review priority (SM2)</h2>
      <div id="reviewBox"><div class="loading">loading...</div></div>
    </div>
    <div class="card">
      <h2>recent records</h2>
      <div id="recentBox"><div class="loading">loading...</div></div>
    </div>
  </div>
  <div class="btn-row">
    <button class="btn" onclick="location.reload()">refresh</button>
  </div>
  <div class="note">click a recent record to view its conversation</div>
</div>

<div class="modal-mask" id="detailMask">
  <div class="modal">
    <div class="modal-head">
      <h3 id="detailTitle">conversation</h3>
      <button class="modal-close" onclick="closeDetail()">✕</button>
    </div>
    <div class="modal-body" id="detailBody"><div class="modal-loading">loading...</div></div>
  </div>
</div>

<script>
var dimMap={architecture:"architecture",engineering:"engineering",model:"model",rag:"RAG","multi-agent":"multi-agent",evaluation:"evaluation","full-stack":"full-stack"};
var recentRecords=[];
fetch("/api/diagnosis",{credentials:"include"}).then(function(r){return r.json()}).then(render).catch(function(e){document.getElementById("statsBox").innerHTML="<div class=empty>failed</div>"});
function render(d){
  var sc=d.dimensionScores||[];
  if(!sc.length){document.getElementById("statsBox").innerHTML="<div class=empty>no data</div>";return}
  new Chart(document.getElementById("radarChart"),{type:"radar",data:{labels:sc.map(function(s){return dimMap[s.dimension]||s.dimension}),datasets:[{label:"score",data:sc.map(function(s){return s.score||0}),backgroundColor:"rgba(14,165,233,0.12)",borderColor:"#0ea5e9",borderWidth:2,pointBackgroundColor:"#0ea5e9",pointRadius:4}]},options:{responsive:true,scales:{r:{beginAtZero:true,max:10,ticks:{stepSize:2},pointLabels:{font:{size:12}}}},plugins:{legend:{display:false}}}});
  var avg=d.avgScore||0,cls=avg>=7?"#10b981":avg>=5?"#f59e0b":"#ef4444";
  var weaks=(d.weakDimensions||[]).map(function(w){return dimMap[w]||w});
  document.getElementById("statsBox").innerHTML="<div style=display:grid;grid-template-columns:1fr 1fr;gap:12px>"+
    "<div style=background:#f8fafc;border:1px solid #e2e8f0;border-radius:10px;padding:14px><div style=font-size:12px;color:#64748b>total</div><div style=font-size:22px;font-weight:700>"+(d.totalAnswered||0)+"</div></div>"+
    "<div style=background:#f8fafc;border:1px solid #e2e8f0;border-radius:10px;padding:14px><div style=font-size:12px;color:#64748b>avg</div><div style=font-size:22px;font-weight:700;color:"+cls+">"+avg.toFixed(1)+"</div></div>"+
    "<div style=background:#f8fafc;border:1px solid #e2e8f0;border-radius:10px;padding:14px><div style=font-size:12px;color:#64748b>weak</div><div style=font-size:14px;font-weight:500;color:#ef4444>"+(weaks.length?weaks.join(", "):"none")+"</div></div></div>";
  var rv=d.reviewPriority||[];
  var rvEl=document.getElementById("reviewBox");
  if(!rv.length){rvEl.innerHTML="<div style=padding:12px;border-radius:8px;background:#f0fdf4;border:1px solid #bbf7d0;font-size:13px>all clear</div>"}
  else{rvEl.innerHTML=rv.map(function(r){var u=r.urgency>=8;var bg=u?"#fef2f2":r.urgency>=3?"#fffbeb":"#f0fdf4";var brd=u?"#fecaca":r.urgency>=3?"#fde68a":"#bbf7d0";var days=r.daysUntilReview!=null?(r.daysUntilReview<=0?"now":r.daysUntilReview+"d"):"";var lb=u?"urgent":r.urgency>=3?"soon":"ok";return "<div style=padding:10px 12px;margin-bottom:6px;border-radius:8px;background:"+bg+";border:1px solid "+brd+";font-size:13px;display:flex;justify-content:space-between><span>"+(dimMap[r.dimension]||r.dimension)+" "+days+"</span><span style=padding:2px 8px;border-radius:99px;font-size:11px;font-weight:600;background:"+(u?"#fecaca":"#fde68a")+";color:"+(u?"#991b1b":"#92400e")+">"+lb+"</span></div>"}).join("")}
  recentRecords=d.recent||[];
  var rcEl=document.getElementById("recentBox");
  if(!recentRecords.length){rcEl.innerHTML="<div class=empty>no records</div>"}
  else{rcEl.innerHTML=recentRecords.map(function(r,i){var cl=r.score>=7?"#10b981":r.score>=5?"#f59e0b":"#ef4444";var dim=dimMap[r.dimension]||r.dimension;return "<div class=rec-item onclick=showDetail("+i+")><span class=q title=\""+esc(r.question||"")+"\">"+dim+": "+esc(r.question||"")+"</span><span class=s style=color:"+cl+">"+r.score+"</span></div>"}).join("")}
}
function esc(s){return (s||"").replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;")}
function closeDetail(){document.getElementById("detailMask").classList.remove("open")}
document.getElementById("detailMask").addEventListener("click",function(e){if(e.target===this)closeDetail()});
function showDetail(i){
  var r=recentRecords[i];if(!r)return;
  document.getElementById("detailTitle").textContent=(dimMap[r.dimension]||r.dimension)+" · "+r.score+"/10";
  var body=document.getElementById("detailBody");
  body.innerHTML="<div class=modal-loading>loading conversation...</div>";
  document.getElementById("detailMask").classList.add("open");
  if(!r.sessionId){body.innerHTML="<div class=empty>no conversation linked to this record</div>";return}
  fetch("/api/session?id="+encodeURIComponent(r.sessionId),{credentials:"include"}).then(function(res){return res.json()}).then(function(s){
    var msgs=s.messages||[];
    if(!msgs.length){body.innerHTML="<div class=empty>no messages in session</div>";return}
    body.innerHTML=msgs.map(function(m){
      var role=(m.role||"").toLowerCase();
      if(role==="system")return "";
      var who=role==="user"?"candidate":role==="assistant"?"interviewer":"tool";
      return "<div class=\"chat-msg "+role+"\"><div class=who>"+who+"</div><div class=bubble>"+esc(m.content||"")+"</div></div>";
    }).join("");
    body.scrollTop=body.scrollHeight;
  }).catch(function(e){body.innerHTML="<div class=empty>failed to load: "+esc(e.message)+"</div>"});
}
</script>
</body>
</html>`
