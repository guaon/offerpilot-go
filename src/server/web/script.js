// ===== state =====
var STORAGE={sessions:'offerpilot_sessions',current:'offerpilot_current',model:'offerpilot_model',memory:'offerpilot_memory'};
var MODELS=[{id:'deepseek-v4-flash',label:'deepseek-v4-flash'},{id:'deepseek-v3',label:'deepseek-v3'},{id:'claude-sonnet-4.5',label:'claude-sonnet-4.5'},{id:'gpt-4o',label:'gpt-4o'}];
var state={sessions:{},order:[],currentSid:'',model:MODELS[0].id,streaming:false,memoryOn:true};
function uid(){return 's_'+Date.now().toString(36)+'_'+Math.random().toString(36).slice(2,7);}
function $(id){return document.getElementById(id);}
var welcome=$('welcome'),chat=$('chat'),input=$('input'),composer=$('composer'),sendBtn=$('sendBtn');
var modelBtn=$('modelBtn'),modelLabel=$('modelLabel'),modelMenu=$('modelMenu'),memoryToggle=$('memoryToggle');
var memoryBar=$('memoryBar'),sessionsBar=$('sessionsBar'),lastSessionBtn=$('lastSession'),historyChips=$('historyChips'),newSessionBtn=$('newSessionBtn');
function loadAll(){
  try{state.sessions=JSON.parse(localStorage.getItem(STORAGE.sessions)||'{}');}catch(e){state.sessions={};}
  state.currentSid=localStorage.getItem(STORAGE.current)||'';
  state.model=localStorage.getItem(STORAGE.model)||MODELS[0].id;
  state.memoryOn=localStorage.getItem(STORAGE.memory)!=='off';
  rebuildOrder();
}
function saveSessions(){try{localStorage.setItem(STORAGE.sessions,JSON.stringify(state.sessions));}catch(e){}}
function rebuildOrder(){state.order=Object.keys(state.sessions).sort(function(a,b){return (state.sessions[b].updatedAt||0)-(state.sessions[a].updatedAt||0);});}
function getSession(sid){return state.sessions[sid];}
function createSession(){
  var sid=uid();
  state.sessions[sid]={id:sid,title:'新会谈',messages:[],createdAt:Date.now(),updatedAt:Date.now()};
  state.currentSid=sid;saveSessions();localStorage.setItem(STORAGE.current,sid);rebuildOrder();
  return state.sessions[sid];
}
function autoTitle(text){var t=(text||'').trim().replace(/\s+/g,' ');return t.length>20?t.slice(0,20)+'…':t||'新会谈';}
function relTime(ts){
  var d=Date.now()-ts;
  if(d<60000)return '刚刚';
  if(d<3600000)return Math.floor(d/60000)+' 分钟前';
  if(d<86400000)return Math.floor(d/3600000)+' 小时前';
  if(d<7*86400000)return Math.floor(d/86400000)+' 天前';
  var date=new Date(ts);return (date.getMonth()+1)+'/'+date.getDate();
}
function escapeHtml(s){return (s||'').replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
function renderMarkdown(text){
  var safe=escapeHtml(text||'');
  safe=safe.replace(/```([\s\S]*?)```/g,function(m,code){return '<pre><code>'+code+'</code></pre>';});
  safe=safe.replace(/`([^`]+)`/g,'<code>$1</code>');
  safe=safe.replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
  safe=safe.replace(/\n/g,'<br/>');
  return safe;
}

// ===== render =====
function renderModelMenu(){
  modelLabel.textContent=state.model;
  modelMenu.innerHTML='';
  MODELS.forEach(function(m){
    var b=document.createElement('button');
    if(m.id===state.model)b.classList.add('active');
    b.innerHTML='<span>'+escapeHtml(m.label)+'</span>'+(m.id===state.model?'<span class="check">✓</span>':'');
    b.addEventListener('click',function(){
      state.model=m.id;
      localStorage.setItem(STORAGE.model,m.id);
      modelLabel.textContent=m.id;
      modelMenu.classList.add('hidden');
      renderModelMenu();
    });
    modelMenu.appendChild(b);
  });
}
function renderSessionsBar(){
  rebuildOrder();
  var others=state.order.filter(function(sid){return sid!==state.currentSid;});
  if(others.length===0){sessionsBar.classList.add('hidden');return;}
  sessionsBar.classList.remove('hidden');
  var last=state.sessions[others[0]];
  lastSessionBtn.classList.remove('hidden');
  lastSessionBtn.innerHTML='<span class="arrow">↩</span><span class="title">'+escapeHtml(last.title)+'</span><span class="ts">· '+relTime(last.updatedAt)+'</span>';
  lastSessionBtn.onclick=function(){switchSession(last.id);};
  historyChips.innerHTML='';
  others.slice(1,8).forEach(function(sid){
    var s=state.sessions[sid];
    var chip=document.createElement('button');
    chip.className='history-chip';
    chip.textContent=s.title;
    chip.title=s.title+' · '+relTime(s.updatedAt);
    chip.onclick=function(){switchSession(sid);};
    historyChips.appendChild(chip);
  });
}
function switchSession(sid){
  if(!state.sessions[sid])return;
  state.currentSid=sid;
  localStorage.setItem(STORAGE.current,sid);
  var s=state.sessions[sid];
  renderMessages(s.messages);
  renderSessionsBar();
}
function renderMessages(messages){
  welcome.classList.add('hidden');
  chat.classList.remove('hidden');
  chat.innerHTML='';
  (messages||[]).forEach(function(m){appendMessageEl(m.role,m.content,{streaming:false,thinking:m.thinking||''});});
  scrollToBottom();
}
function appendMessageEl(role,content,opts){
  opts=opts||{};
  var div=document.createElement('div');
  div.className='msg '+role;
  if(opts.streaming)div.classList.add('streaming');
  if(opts.thinking&&!content)div.classList.add('thinking');
  var avatar=document.createElement('div');
  avatar.className='msg-avatar';
  avatar.textContent=role==='user'?'我':'OP';
  div.appendChild(avatar);
  var body=document.createElement('div');
  body.className='msg-body';
  if(role==='assistant')body.innerHTML=renderMarkdown(content);
  else body.textContent=content;
  div.appendChild(body);
  chat.appendChild(div);
  if(!opts.streaming)scrollToBottom();
  return {div:div,body:body};
}
function scrollToBottom(){requestAnimationFrame(function(){chat.scrollTop=chat.scrollHeight;});}
function updateStreamingContent(accum){
  var last=chat.querySelector('.msg.assistant.streaming');
  if(!last)return;
  var body=last.querySelector('.msg-body');
  if(accum.text){last.classList.remove('thinking');body.innerHTML=renderMarkdown(accum.text);}
  else if(accum.thinking){last.classList.add('thinking');body.textContent=accum.thinking;}
  scrollToBottom();
}
function finishStreamingContent(accum){
  var last=chat.querySelector('.msg.assistant.streaming');
  if(!last)return;
  last.classList.remove('streaming');
  var body=last.querySelector('.msg-body');
  body.innerHTML=renderMarkdown(accum.text);
  scrollToBottom();
}
function appendSystemMessage(text){
  var div=document.createElement('div');
  div.style.cssText='text-align:center;font-size:12px;color:#a8a29e;padding:8px;';
  div.textContent=text;
  chat.appendChild(div);
  scrollToBottom();
}
function appendToolEvent(name,input){
  var div=document.createElement('div');
  div.className='tool-event';
  var s=input?JSON.stringify(input).slice(0,200):'';
  div.innerHTML='<span class="tool-event-name">+ '+escapeHtml(name)+'</span> '+escapeHtml(s);
  chat.appendChild(div);
  scrollToBottom();
}
function appendToolResult(name,result){
  var div=document.createElement('div');
  div.className='tool-event';
  var s=(result||'').slice(0,200);
  div.innerHTML='<span class="tool-event-name">v '+escapeHtml(name)+'</span> '+escapeHtml(s);
  chat.appendChild(div);
  scrollToBottom();
}
function updateMemoryBar(){
  var lbl=memoryToggle.querySelector('.memory-label');
  if(state.memoryOn){lbl.textContent='记忆调用 本地开';memoryToggle.classList.remove('off');
    memoryBar.classList.remove('off');
    memoryBar.querySelector('strong').textContent='本次不调用';
    memoryBar.querySelector('span').textContent='当前消息会发送给 OfferPilot 的服务器和 AI 服务生成回复；未登录时，会谈历史和记忆资产保留在本浏览器，不进入账号记忆。';
  }else{
    lbl.textContent='记忆调用 已关闭';
    memoryToggle.classList.add('off');
    memoryBar.classList.add('off');
    memoryBar.querySelector('strong').textContent='本次调用记忆';
    memoryBar.querySelector('span').textContent='当前会谈会带上这台浏览器里的本地记忆；登录后才会保存到账号。';
  }
}

// ===== submit & stream =====
async function submit(){
  var text=input.value.trim();
  if(!text||state.streaming)return;
  if(!state.currentSid)createSession();
  var session=getSession(state.currentSid);
  if(!session)return;
  var userMsg={role:'user',content:text};
  session.messages.push(userMsg);
  session.updatedAt=Date.now();
  if(session.title==='新会谈'||!session.title)session.title=autoTitle(text);
  saveSessions();
  renderSessionsBar();
  welcome.classList.add('hidden');
  chat.classList.remove('hidden');
  appendMessageEl('user',text,{streaming:false});
  input.value='';input.style.height='auto';
  appendMessageEl('assistant','',{streaming:true});
  state.streaming=true;sendBtn.disabled=true;
  var accum={text:'',thinking:''};
  try{
    await streamChat(text,function(evt){handleEvent(evt,accum,session);},function(){onStreamDone(accum,session);});
  }catch(e){
    appendSystemMessage('错误: '+(e.message||'网络异常'));
  }finally{
    state.streaming=false;sendBtn.disabled=false;input.focus();
  }
}
async function streamChat(text,onEvent,onDone){
  var res=await fetch('/api/chat',{
    method:'POST',
    headers:{'Content-Type':'application/json',...(state.currentSid?{'X-Session-ID':state.currentSid}:{})},
    credentials:'include',
    body:JSON.stringify({message:text,sessionId:state.currentSid||'',model:state.model,memory:state.memoryOn}),
  });
  if(!res.ok||!res.body)throw new Error('HTTP '+res.status);
  var reader=res.body.getReader();
  var decoder=new TextDecoder('utf-8');
  var buffer='';
  while(true){
    var r=await reader.read();
    if(r.done)break;
    buffer+=decoder.decode(r.value,{stream:true});
    var parts=buffer.split('\n\n');
    buffer=parts.pop();
    for(var i=0;i<parts.length;i++){
      var line=parts[i].trim();
      if(!line.startsWith('data:'))continue;
      var payload=line.slice(5).trim();
      if(payload==='[DONE]')continue;
      try{
        var evt=JSON.parse(payload);
        onEvent(evt);
        if(evt.sessionId){state.currentSid=evt.sessionId;localStorage.setItem(STORAGE.current,state.currentSid);}
      }catch(e){console.warn('parse fail:',payload,e);}
    }
  }
  if(onDone)onDone();
}
function handleEvent(evt,accum,session){
  switch(evt.type){
    case 'text_delta':accum.text+=evt.content||'';updateStreamingContent(accum);break;
    case 'thinking_delta':accum.thinking+=evt.content||'';updateStreamingContent(accum);break;
    case 'tool_call':appendToolEvent(evt.name,evt.input);break;
    case 'tool_result':appendToolResult(evt.name,evt.result);break;
    case 'error':appendSystemMessage('错误: '+(evt.error||'未知错误'));break;
  }
  scrollToBottom();
}
function onStreamDone(accum,session){
  finishStreamingContent(accum);
  session.messages.push({role:'assistant',content:accum.text,thinking:accum.thinking||''});
  session.updatedAt=Date.now();
  saveSessions();
  renderSessionsBar();
}
// ===== events =====
function bindEvents(){
  input.addEventListener('input',function(){input.style.height='auto';input.style.height=Math.min(input.scrollHeight,180)+'px';});
  input.addEventListener('keydown',function(e){if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();submit();}});
  sendBtn.addEventListener('click',submit);
  composer.addEventListener('submit',function(e){e.preventDefault();submit();});
  document.querySelectorAll('.chip').forEach(function(btn){
    btn.addEventListener('click',function(){
      var p=btn.getAttribute('data-prompt')||btn.textContent;
      input.value=p;input.focus();input.style.height='auto';submit();
    });
  });
  modelBtn.addEventListener('click',function(e){e.stopPropagation();modelMenu.classList.toggle('hidden');});
  document.addEventListener('click',function(){modelMenu.classList.add('hidden');});
  modelMenu.addEventListener('click',function(e){e.stopPropagation();});
  memoryToggle.addEventListener('click',function(){
    state.memoryOn=!state.memoryOn;
    localStorage.setItem(STORAGE.memory,state.memoryOn?'on':'off');
    updateMemoryBar();
  });
  newSessionBtn.addEventListener('click',function(){
    var s=createSession();
    welcome.classList.remove('hidden');
    chat.classList.add('hidden');
    chat.innerHTML='';
    renderSessionsBar();
  });
}
// ===== init =====
function init(){
  loadAll();
  renderModelMenu();
  updateMemoryBar();
  bindEvents();
  if(state.currentSid&&state.sessions[state.currentSid]){
    renderMessages(state.sessions[state.currentSid].messages);
  }else{
    welcome.classList.remove('hidden');
    chat.classList.add('hidden');
  }
  renderSessionsBar();
}
if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',init);}else{init();}

