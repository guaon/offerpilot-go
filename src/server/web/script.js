// ===== state =====
var STORAGE={sessions:'offerpilot_sessions',current:'offerpilot_current',model:'offerpilot_model',collapsed:'offerpilot_sidebar_collapsed'};
var MODELS=[{id:'deepseek-v4-flash',label:'deepseek-v4-flash'},{id:'deepseek-v3',label:'deepseek-v3'},{id:'claude-sonnet-4.5',label:'claude-sonnet-4.5'},{id:'gpt-4o',label:'gpt-4o'}];
var state={sessions:{},order:[],currentSid:'',model:MODELS[0].id,streaming:false,files:[]};
var resumePageImages=[];
function uid(){return 's_'+Date.now().toString(36)+'_'+Math.random().toString(36).slice(2,7);}
function $(id){return document.getElementById(id);}
var welcome=$('welcome'),chat=$('chat'),input=$('input'),composer=$('composer'),sendBtn=$('sendBtn');
var modelBtn=$('modelBtn'),modelLabel=$('modelLabel'),modelMenu=$('modelMenu');
var sessionsBar=$('sessionsBar'),lastSessionBtn=$('lastSession'),historyChips=$('historyChips'),newSessionBtn=$('newSessionBtn');
var sidebar=$('sidebar'),sessionGroups=$('sessionGroups'),newChatBtnTop=$('newChatBtnTop'),sidebarToggle=$('sidebarToggle'),sidebarClose=$('sidebarClose'),sessionCount=$('sessionCount');
var radarView=$('radarView'),radarBack=$('radarBack'),radarRefresh=$('radarRefresh'),radarCanvas=$('radarCanvas'),radarTabs=$('radarTabs'),radarTitle=$('radarTitle');
var statAvg=$('statAvg'),statTotal=$('statTotal'),statWeak=$('statWeak'),statSum=$('statSum');
var dimList=$('dimList'),readinessFill=$('readinessFill'),readinessValue=$('readinessValue'),learnGrid=$('learnGrid');
var panelRadar=$('panelRadar'),panelResume=$('panelResume'),panelMatch=$('panelMatch');
var resumeInput=$('resumeInput'),resumeDiagnoseBtn=$('resumeDiagnoseBtn'),resumeAIBtn=$('resumeAIBtn'),resumeStatus=$('resumeStatus'),resumeResults=$('resumeResults');
var jdInput=$('jdInput'),matchResumeInput=$('matchResumeInput'),matchBtn=$('matchBtn'),matchAIBtn=$('matchAIBtn'),matchStatus=$('matchStatus'),matchResults=$('matchResults');
var resumeFileInput=$('resumeFileInput'),resumeUploadBtn=$('resumeUploadBtn'),resumeUploadStatus=$('resumeUploadStatus');
var jdFileInput=$('jdFileInput'),jdUploadBtn=$('jdUploadBtn'),jdUploadStatus=$('jdUploadStatus');
var matchResumeFileInput=$('matchResumeFileInput'),matchResumeUploadBtn=$('matchResumeUploadBtn'),matchResumeUploadStatus=$('matchResumeUploadStatus');
var authLink=$('auth-link');
var dock=document.querySelector('.dock');
var chatFileInput=$('chatFileInput'),uploadBtn=$('uploadBtn'),fileChips=$('fileChips');
function loadAll(){
  try{state.sessions=JSON.parse(localStorage.getItem(STORAGE.sessions)||'{}');}catch(e){state.sessions={};}
  state.currentSid=localStorage.getItem(STORAGE.current)||'';
  state.model=localStorage.getItem(STORAGE.model)||MODELS[0].id;
  rebuildOrder();
  // 从服务端同步会话列表，合并到本地
  fetchSessionsFromServer();
}
function fetchSessionsFromServer(){
  fetch('/api/sessions',{credentials:'include'})
    .then(function(r){if(!r.ok)throw new Error('HTTP '+r.status);return r.json();})
    .then(function(data){
      var list=data.sessions||[];
      var changed=false;
      list.forEach(function(s){
        // 服务端有的会话但本地没有，补充到本地
        if(!state.sessions[s.id]){
          state.sessions[s.id]={id:s.id,title:'会话',messages:[],createdAt:s.updatedAt||0,updatedAt:s.updatedAt||0};
          changed=true;
        }
      });
      if(changed){
        saveSessions();
        rebuildOrder();
        renderSidebar();
        renderSessionsBar();
      }
    })
    .catch(function(){}); // 服务端不可用时静默降级到 localStorage
}
function saveSessions(){try{localStorage.setItem(STORAGE.sessions,JSON.stringify(state.sessions));}catch(e){}}
function rebuildOrder(){state.order=Object.keys(state.sessions).sort(function(a,b){return (state.sessions[b].updatedAt||0)-(state.sessions[a].updatedAt||0);});}
function getSession(sid){return state.sessions[sid];}
function createSession(){
  // 若当前会话存在且为空（无消息），直接复用
  var cur=state.sessions[state.currentSid];
  if(cur && (!cur.messages || cur.messages.length===0)){
    cur.updatedAt=Date.now();
    saveSessions();rebuildOrder();
    return cur;
  }
  var sid=uid();
  state.sessions[sid]={id:sid,title:'新会谈',messages:[],createdAt:Date.now(),updatedAt:Date.now()};
  state.currentSid=sid;saveSessions();localStorage.setItem(STORAGE.current,sid);rebuildOrder();
  // 异步调用服务端创建会话，拿到 sessionId 后替换本地 ID
  fetch('/api/session/new',{method:'POST',credentials:'include'})
    .then(function(r){if(r.ok)return r.json();return null;})
    .then(function(d){
      // 仅当用户没有切到其他会话时才替换
      if(d&&d.sessionId&&d.sessionId!==sid&&state.currentSid===sid&&state.sessions[sid]){
        state.sessions[d.sessionId]=state.sessions[sid];
        state.sessions[d.sessionId].id=d.sessionId;
        delete state.sessions[sid];
        state.currentSid=d.sessionId;
        localStorage.setItem(STORAGE.current,d.sessionId);
        saveSessions();rebuildOrder();renderSidebar();renderSessionsBar();
      }
    })
    .catch(function(){});
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
  // 先处理 rich block 和代码块（保持原有逻辑）
  safe=safe.replace(/```rich\n([\s\S]*?)```/g,function(m,json){return renderRichBlock(json);});
  safe=safe.replace(/```([\s\S]*?)```/g,function(m,code){return '<pre><code>'+code+'</code></pre>';});

  // 按行分割，逐行解析块级语法
  var lines=safe.split('\n');
  var blocks=[];
  var i=0;
  while(i<lines.length){
    var line=lines[i];
    // 空行作为段落分隔
    if(line.trim()===''){
      i++;
      continue;
    }
    // 水平线
    if(/^-{3,}$/.test(line.trim())){
      blocks.push('<hr/>');
      i++;
      continue;
    }
    // 标题
    var hMatch=line.match(/^(#{1,3})\s+(.+)/);
    if(hMatch){
      var level=hMatch[1].length;
      var content=hMatch[2];
      // 对标题内容做 inline 格式化
      content=formatInline(content);
      blocks.push('<h'+level+'>'+content+'</h'+level+'>');
      i++;
      continue;
    }
    // 无序列表项
    var ulMatch=line.match(/^(\s*)[-*+]\s+(.+)/);
    if(ulMatch){
      var items=[];
      while(i<lines.length){
        var m=lines[i].match(/^(\s*)[-*+]\s+(.+)/);
        if(m){
          items.push('<li>'+formatInline(m[2])+'</li>');
          i++;
        }else break;
      }
      blocks.push('<ul>'+items.join('')+'</ul>');
      continue;
    }
    // 有序列表项
    var olMatch=line.match(/^(\s*)(\d+)\.\s+(.+)/);
    if(olMatch){
      var olItems=[];
      while(i<lines.length){
        var om=lines[i].match(/^(\s*)(\d+)\.\s+(.+)/);
        if(om){
          olItems.push('<li>'+formatInline(om[3])+'</li>');
          i++;
        }else break;
      }
      blocks.push('<ol>'+olItems.join('')+'</ol>');
      continue;
    }
    // 表格行（连续以 | 分隔的行，且至少有一个分隔行）
    if(/^\s*\|/.test(line)&&/\|\s*$/.test(line.trim())){
      // 向前查找是否存在分隔行
      var tmpI=i;
      var hasSep=false;
      while(tmpI<lines.length){
        var tl=lines[tmpI].trim();
        if(tl==='')break;
        if(/^\|[\s\-:]+\|$/.test(tl)){hasSep=true;break;}
        if(!/^\s*\|/.test(tl)||!/\|\s*$/.test(tl))break;
        tmpI++;
      }
      if(hasSep){
        var headerRow=parseTableRow(line);
        i++;
        // 吃掉分隔行
        if(i<lines.length&&/^\|[\s\-:]+\|$/.test(lines[i].trim()))i++;
        var dataRows=[];
        while(i<lines.length){
          var dl=lines[i].trim();
          if(dl===''||!/^\s*\|/.test(dl)||!/\|\s*$/.test(dl))break;
          dataRows.push(parseTableRow(dl));
          i++;
        }
        // 生成表格 HTML
        var tblHtml='<table class="md-table"><thead><tr>';
        headerRow.forEach(function(c){tblHtml+='<th>'+formatInline(c)+'</th>';});
        tblHtml+='</tr></thead><tbody>';
        dataRows.forEach(function(row){
          tblHtml+='<tr>';
          row.forEach(function(c){tblHtml+='<td>'+formatInline(c)+'</td>';});
          tblHtml+='</tr>';
        });
        tblHtml+='</tbody></table>';
        blocks.push(tblHtml);
        continue;
      }
    }
    // 普通段落（连续非空、非块级行合并为一个段落）
    var paraLines=[];
    while(i<lines.length){
      var pl=lines[i];
      if(pl.trim()==='' || /^#{1,3}\s+/.test(pl) || /^-{3,}$/.test(pl.trim()) ||
         /^(\s*)[-*+]\s+/.test(pl) || /^(\s*)\d+\.\s+/.test(pl) ||
         (/^\s*\|/.test(pl)&&/\|\s*$/.test(pl.trim()))){
        break;
      }
      paraLines.push(pl);
      i++;
    }
    if(paraLines.length){
      var paraText=paraLines.join(' ');
      paraText=formatInline(paraText);
      blocks.push('<p>'+paraText+'</p>');
    }
  }
  return blocks.join('');
}
// inline 格式化：加粗、行内代码
function formatInline(text){
  text=text.replace(/`([^`]+)`/g,'<code>$1</code>');
  text=text.replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
  return text;
}
// 解析表格行，按 | 分割单元格（去除首尾空单元格）
function parseTableRow(line){
  var cells=line.split('|');
  // 去除首尾空元素（因为 | 开头和结尾会产生空字符串）
  if(cells.length&&cells[0].trim()==='')cells.shift();
  if(cells.length&&cells[cells.length-1].trim()==='')cells.pop();
  return cells.map(function(c){return c.trim();});
}
function renderRichBlock(json){
  try{
    var d=JSON.parse(json);
    switch(d.type){
      case 'match_matrix': return renderRichMatchMatrix(d);
      case 'table': return renderRichTable(d);
      case 'score': return renderRichScore(d);
      case 'cards': return renderRichCards(d);
      default: return '<pre><code>'+escapeHtml(json)+'</code></pre>';
    }
  }catch(e){return '<pre><code>'+escapeHtml(json)+'</code></pre>';}
}
function renderRichMatchMatrix(d){
  var html='<div class="rich-block rich-match-matrix">';
  if(d.title)html+='<div class="rich-title">'+escapeHtml(d.title)+'</div>';
  html+='<table class="rich-table"><thead><tr>';
  (d.columns||[]).forEach(function(c){html+='<th>'+escapeHtml(c)+'</th>';});
  html+='</tr></thead><tbody>';
  (d.rows||[]).forEach(function(row){
    html+='<tr>';
    row.forEach(function(cell,i){
      var cls='';
      var content=escapeHtml(String(cell));
      if(content.indexOf('🟢')===0||content.indexOf('✅')===0)cls=' class="cell-match"';
      else if(content.indexOf('🟡')===0||content.indexOf('⚠')===0)cls=' class="cell-partial"';
      else if(content.indexOf('🔴')===0||content.indexOf('❌')===0)cls=' class="cell-gap"';
      html+='<td'+cls+'>'+content+'</td>';
    });
    html+='</tr>';
  });
  html+='</tbody></table></div>';
  return html;
}
function renderRichTable(d){
  var html='<div class="rich-block rich-table-wrap">';
  if(d.title)html+='<div class="rich-title">'+escapeHtml(d.title)+'</div>';
  html+='<table class="rich-table"><thead><tr>';
  (d.columns||[]).forEach(function(c){html+='<th>'+escapeHtml(c)+'</th>';});
  html+='</tr></thead><tbody>';
  (d.rows||[]).forEach(function(row){
    html+='<tr>';
    row.forEach(function(cell){html+='<td>'+escapeHtml(String(cell))+'</td>';});
    html+='</tr>';
  });
  html+='</tbody></table></div>';
  return html;
}
function renderRichScore(d){
  var pct=(d.score||0)/(d.total||10)*100;
  var color=pct>=70?'#16a34a':pct>=40?'#d97706':'#b91c1c';
  return '<div class="rich-block rich-score">'+
    '<div class="rich-score-circle" style="background:conic-gradient('+color+' '+pct+'%, #f1f5f9 '+pct+'%)">'+
    '<span class="rich-score-num">'+(d.score||0)+'<small>/'+(d.total||10)+'</small></span></div>'+
    '<div class="rich-score-label">'+escapeHtml(d.label||'')+'</div></div>';
}
function renderRichCards(d){
  var html='<div class="rich-block rich-cards">';
  if(d.title)html+='<div class="rich-title">'+escapeHtml(d.title)+'</div>';
  (d.items||[]).forEach(function(item){
    html+='<div class="rich-card"><div class="rich-card-title">'+escapeHtml(item.title||'')+'</div>';
    html+='<div class="rich-card-body">'+escapeHtml(item.body||'')+'</div></div>';
  });
  html+='</div>';
  return html;
}

// ===== sidebar =====
function bucketOf(ts){
  var now=new Date();
  var d=new Date(ts);
  var sameDay=now.toDateString()===d.toDateString();
  if(sameDay)return 'today';
  var y=new Date(now);y.setDate(now.getDate()-1);
  if(y.toDateString()===d.toDateString())return 'yesterday';
  var diff=(now-d)/(1000*60*60*24);
  if(diff<=7)return 'week';
  if(diff<=30)return 'month';
  return 'older';
}
var BUCKET_LABEL={today:'今天',yesterday:'昨天',week:'7天内',month:'30天内',older:'更早'};
var BUCKET_ORDER=['today','yesterday','week','month','older'];
function renderSidebar(){
  if(!sessionGroups)return;
  rebuildOrder();
  var byBucket={};
  state.order.forEach(function(sid){
    var s=state.sessions[sid];
    if(!s)return;
    var b=bucketOf(s.updatedAt||s.createdAt||0);
    if(!byBucket[b])byBucket[b]=[];
    byBucket[b].push(sid);
  });

  var html='';
  var anyRendered=false;
  BUCKET_ORDER.forEach(function(b){
    var sids=byBucket[b];
    if(!sids||sids.length===0)return;
    anyRendered=true;
    html+='<div class="group" data-bucket="'+b+'">';
    html+='<div class="group-label">'+BUCKET_LABEL[b]+'</div>';
    html+='<div class="group-list">';
    sids.forEach(function(sid){
      var s=state.sessions[sid];
      var active=sid===state.currentSid?' active':'';
      html+='<button class="session-item'+active+'" data-sid="'+sid+'" type="button">';
      html+='<span class="session-icon">💬</span>';
      html+='<span class="session-title">'+escapeHtml(s.title||'新会话')+'</span>';
      html+='<span class="session-actions">';
      html+='<span class="session-action-btn session-del-btn" data-sid="'+sid+'" title="删除会话" role="button">✕</span>';
      html+='</span></button>';
    });
    html+='</div></div>';
  });

  if(!anyRendered){
    html='<div class="sidebar-empty">还没有会话<br/>点击上方按钮开始你的第一次诊断</div>';
  }
  sessionGroups.innerHTML=html;
  if(sessionCount){
    var total=state.order.length;
    sessionCount.textContent=total===0?'还没有会话':total+' 个会话';
  }
}
function switchToSession(sid){
  if(!state.sessions[sid])return;
  switchSession(sid);
  renderSidebar();
}
function createAndRenderSession(){
  var s=createSession();
  welcome.classList.remove('hidden');
  chat.classList.add('hidden');
  chat.innerHTML='';
  state.files=[];renderFileChips();
  renderSessionsBar();
  renderSidebar();
  setTimeout(function(){try{input.focus();}catch(e){}},0);
  return s;
}
function deleteSession(sid,ev){
  if(ev){ev.stopPropagation();ev.preventDefault();}
  var s=state.sessions[sid];
  if(!s)return;
  var remaining=Object.keys(state.sessions).length;
  if(remaining<=1){
    // 唯一一个会话：清空内容，不删除
    s.title='新会话';s.messages=[];s.updatedAt=Date.now();
    saveSessions();rebuildOrder();
    state.currentSid=sid;
    localStorage.setItem(STORAGE.current,sid);
    welcome.classList.remove('hidden');chat.classList.add('hidden');chat.innerHTML='';
    renderSessionsBar();renderSidebar();
    return;
  }
  delete state.sessions[sid];
  if(state.currentSid===sid){
    var next=Object.keys(state.sessions)[0];
    if(next){state.currentSid=next;localStorage.setItem(STORAGE.current,next);}else{
      createSession();welcome.classList.remove('hidden');chat.classList.add('hidden');chat.innerHTML='';
    }
  }
  saveSessions();rebuildOrder();renderSessionsBar();renderSidebar();
  if(state.currentSid&&state.sessions[state.currentSid]){
    renderMessages(state.sessions[state.currentSid].messages);
  }
  // 通知服务端删除
  fetch('/api/session?id='+encodeURIComponent(sid),{method:'DELETE',credentials:'include'}).catch(function(){});
}
function bindSidebar(){
  if(sessionGroups){
    sessionGroups.addEventListener('click',function(e){
      var delBtn=e.target.closest('.session-del-btn');
      if(delBtn){
        var sid=delBtn.getAttribute('data-sid');
        deleteSession(sid,e);
        return;
      }
      var item=e.target.closest('.session-item');
      if(item){
        var sid=item.getAttribute('data-sid');
        switchToSession(sid);
      }
    });
  }
  if(newChatBtnTop){
    newChatBtnTop.addEventListener('click',createAndRenderSession);
  }
  if(sidebarToggle){
    sidebarToggle.addEventListener('click',function(){
      sidebar.classList.remove('collapsed');
      sidebarToggle.classList.add('hidden');
      try{localStorage.setItem(STORAGE.collapsed,'');}catch(e){}
    });
  }
  if(sidebarClose){
    sidebarClose.addEventListener('click',function(){
      sidebar.classList.add('collapsed');
      sidebarToggle.classList.remove('hidden');
      try{localStorage.setItem(STORAGE.collapsed,'1');}catch(e){}
    });
  }
  // 初始化折叠状态
  try{
    if(localStorage.getItem(STORAGE.collapsed)==='1'){
      sidebar.classList.add('collapsed');
      sidebarToggle.classList.remove('hidden');
    }
  }catch(e){}
}
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
  // 切走前持久化当前会话
  saveSessions();
  state.currentSid=sid;
  localStorage.setItem(STORAGE.current,sid);
  var s=state.sessions[sid];
  renderMessages(s.messages);
  renderSessionsBar();
  renderSidebar();
}
function renderMessages(messages){
  welcome.classList.add('hidden');
  chat.classList.remove('hidden');
  chat.innerHTML='';
  (messages||[]).forEach(function(m){appendMessageEl(m.role,m.content,{streaming:false,thinking:m.thinking||'',files:m.files||[]});});
  scrollToBottom();
}
function appendMessageEl(role,content,opts){
  opts=opts||{};
  var div=document.createElement('div');
  div.className='msg '+role;
  if(opts.streaming)div.classList.add('streaming');
  var avatar=document.createElement('div');
  avatar.className='msg-avatar';
  avatar.textContent=role==='user'?'我':'OP';
  div.appendChild(avatar);
  var body=document.createElement('div');
  body.className='msg-body';
  // assistant 消息：如果带 thinking 历史，在 body 顶部先插入折叠块，再放正文容器
  if(role==='assistant'){
    if(opts.thinking){
      var block=document.createElement('details');
      block.className='think-block';
      block.open=false;
      var sum=document.createElement('summary');
      sum.textContent='思考过程';
      block.appendChild(sum);
      // 将 thinking 内容渲染为折叠条目
      var entry=document.createElement('div');
      entry.className='think-entry';
      entry.innerHTML='<span class="think-tag">推理</span> <span class="think-detail">'+escapeHtml(opts.thinking).replace(/\n/g,'<br/>')+'</span>';
      block.appendChild(entry);
      body.appendChild(block);
    }else if(opts.streaming){
      // 流式开始时先占位，等工具调用时再创建（保持空即可）
    }
    var contentWrap=document.createElement('div');
    contentWrap.className='msg-content';
    contentWrap.innerHTML=renderMarkdown(content);
    body.appendChild(contentWrap);
    // 纯思考中状态（还没正文输出）的样式提示
    if(opts.thinking&&!content)div.classList.add('thinking');
  }else{
    // user message: show file chips first, then text
    if(opts.files&&opts.files.length>0){
      var fc=document.createElement('div');
      fc.className='msg-files';
      opts.files.forEach(function(f){
        var chip=document.createElement('span');
        chip.className='file-chip';
        chip.innerHTML='<span class="file-chip-icon">📄</span><span class="file-chip-name">'+escapeHtml(f.name)+'</span><span class="file-chip-size">'+formatSize(f.size)+'</span>';
        fc.appendChild(chip);
      });
      body.appendChild(fc);
    }
    body.appendChild(document.createTextNode(content));
  }
  div.appendChild(body);
  chat.appendChild(div);
  if(!opts.streaming)scrollToBottom();
  return {div:div,body:body};
}
function scrollToBottom(){requestAnimationFrame(function(){chat.scrollTop=chat.scrollHeight;});}
function updateStreamingContent(accum){
  var last=chat.querySelector('.msg.assistant.streaming');
  if(!last)return;
  // 确保 content 容器存在
  if(!last.querySelector('.msg-content')){
    var c=document.createElement('div');
    c.className='msg-content';
    last.querySelector('.msg-body').appendChild(c);
  }
  // 节流：用 RAF 合并高频更新，避免大文本（如简历）时页面卡死
  if(last._rafId)return;
  last._rafId=requestAnimationFrame(function(){
    last._rafId=null;
    var content=last.querySelector('.msg-content');
    if(!content)return;
    if(accum.text){
      last.classList.remove('thinking');
      content.innerHTML=renderMarkdown(accum.text);
    }else if(accum.thinking){
      last.classList.add('thinking');
      var block=last.querySelector('.think-block');
      if(!block)block=getThinkBlock();
      var thinkingEntry=block.querySelector('.think-entry.thinking-main');
      if(!thinkingEntry){
        thinkingEntry=document.createElement('div');
        thinkingEntry.className='think-entry thinking-main';
        thinkingEntry.innerHTML='<span class="think-tag">推理</span> <span class="think-detail"></span>';
        block.appendChild(thinkingEntry);
      }
      var detailEl=thinkingEntry.querySelector('.think-detail');
      if(detailEl)detailEl.innerHTML=escapeHtml(accum.thinking).replace(/\n/g,'<br/>');
    }
    scrollToBottom();
  });
}
function finishStreamingContent(accum){
  var last=chat.querySelector('.msg.assistant.streaming');
  if(!last)return;
  // 取消待处理的 RAF，直接用最终内容渲染
  if(last._rafId){cancelAnimationFrame(last._rafId);last._rafId=null;}
  last.classList.remove('streaming');
  var tb=last.querySelector('.think-block');if(tb)tb.open=false;
  var content=last.querySelector('.msg-content');
  if(!content){
    content=document.createElement('div');
    content.className='msg-content';
    last.querySelector('.msg-body').appendChild(content);
  }
  content.innerHTML=renderMarkdown(accum.text);
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
  var s=input?JSON.stringify(input).slice(0,200):'';
  var entry=document.createElement('div');
  entry.className='think-entry';
  entry.innerHTML='<span class="think-tag">调用</span> '+escapeHtml(name)+' <span class="think-detail">'+escapeHtml(s)+'</span>';
  getThinkBlock().appendChild(entry);
  scrollToBottom();
}
function appendToolResult(name,result){
  var s=(result||'').slice(0,200);
  var entry=document.createElement('div');
  entry.className='think-entry result';
  entry.innerHTML='<span class="think-tag done">完成</span> '+escapeHtml(name)+' <span class="think-detail">'+escapeHtml(s)+'</span>';
  getThinkBlock().appendChild(entry);
}
// 获取或创建当前 assistant 消息中的思考过程折叠块（放在 msg-body 内部顶部，正文容器上方）
function getThinkBlock(){
  var last=chat.querySelector('.msg.assistant.streaming');
  if(!last){
    last=document.createElement('div');
    last.className='msg assistant streaming';
    var avatar=document.createElement('div');
    avatar.className='msg-avatar';avatar.textContent='OP';
    last.appendChild(avatar);
    var body=document.createElement('div');
    body.className='msg-body';
    var contentWrap=document.createElement('div');
    contentWrap.className='msg-content';
    body.appendChild(contentWrap);
    last.appendChild(body);
    chat.appendChild(last);
  }
  var body=last.querySelector('.msg-body');
  var block=last.querySelector('.think-block');
  if(!block){
    block=document.createElement('details');
    block.className='think-block';
    block.open=true;
    var summary=document.createElement('summary');
    summary.textContent='思考过程';
    block.appendChild(summary);
    // 插在 msg-body 内部最顶端（正文容器 msg-content 之前）
    body.insertBefore(block,body.firstChild);
  }
  // 确保存在正文内容容器
  if(!body.querySelector('.msg-content')){
    var cw=document.createElement('div');
    cw.className='msg-content';
    body.appendChild(cw);
  }
  return block;
}

// ===== radar view =====
var DIM_META={
  'architecture':{label:'架构设计',learn:{desc:'学习 Agent 系统分层设计',tag:'理解 10 层 Harness 架构'}},
  'engineering':{label:'工程实践',learn:{desc:'搭建 GitHub Actions CI',tag:'结构化日志'}},
  'model':{label:'模型能力',learn:{desc:'学会 A/B 对比不同模型效果',tag:'模型评估对比'}},
  'rag':{label:'RAG 检索',learn:{desc:'实现 FTS + Vector 混合排序',tag:'混合检索 + Rerank'}},
  'multi-agent':{label:'多 Agent',learn:{desc:'设计子代理分工与协作流程',tag:'Sub-Agent Pool 编排'}},
  'evaluation':{label:'评测体系',learn:{desc:'建立自动化评测与回归测试',tag:'Eval 流水线'}},
  'full-stack':{label:'全栈交付',learn:{desc:'完成端到端功能联调交付',tag:'前后端集成'}},
};
function fetchUser(){
  fetch('/api/me',{credentials:'include'})
    .then(function(r){if(r.ok)return r.json();return null;})
    .then(function(d){state.user=(d&&d.username)?d:null;})
    .catch(function(){});
}
function showRadar(){
  welcome.classList.add('hidden');
  chat.classList.add('hidden');
  if(dock)dock.classList.add('hidden');
  sidebar.classList.add('hidden');
  sidebarToggle.classList.add('hidden');
  radarView.classList.remove('hidden');
  switchRadarTab('radar');
}
function hideRadar(){
  radarView.classList.add('hidden');
  if(dock)dock.classList.remove('hidden');
  var collapsed=localStorage.getItem(STORAGE.collapsed)==='1';
  if(collapsed){
    sidebar.classList.add('collapsed');
    sidebarToggle.classList.remove('hidden');
  }else{
    sidebar.classList.remove('hidden');
    sidebar.classList.remove('collapsed');
  }
  var cur=state.sessions[state.currentSid];
  if(cur&&cur.messages&&cur.messages.length){
    chat.classList.remove('hidden');
  }else{
    welcome.classList.remove('hidden');
  }
}
function switchRadarTab(tab){
  var tabs=radarTabs.querySelectorAll('.radar-tab');
  for(var i=0;i<tabs.length;i++){
    if(tabs[i].getAttribute('data-tab')===tab)tabs[i].classList.add('active');
    else tabs[i].classList.remove('active');
  }
  panelRadar.classList.add('hidden');
  panelResume.classList.add('hidden');
  panelMatch.classList.add('hidden');
  radarRefresh.classList.add('hidden');
  if(tab==='radar'){
    panelRadar.classList.remove('hidden');
    radarTitle.textContent='能力雷达';
    radarRefresh.classList.remove('hidden');
    fetchRadar();
  }else if(tab==='resume'){
    panelResume.classList.remove('hidden');
    radarTitle.textContent='简历诊断';
  }else if(tab==='match'){
    panelMatch.classList.remove('hidden');
    radarTitle.textContent='JD匹配';
  }
}
function jumpToChat(prefill){
  hideRadar();
  // 始终新开会话，不污染已有对话
  createSession();
  renderMessages([]);
  welcome.classList.add('hidden');
  chat.classList.remove('hidden');
  input.value=prefill;
  input.style.height='auto';
  input.style.height=Math.min(input.scrollHeight,180)+'px';
  setTimeout(function(){submit();},200);
}
function fetchRadar(){
  fetch('/api/diagnosis',{credentials:'include'})
    .then(function(r){if(!r.ok)throw new Error('HTTP '+r.status);return r.json();})
    .then(renderRadar)
    .catch(function(e){
      if(dimList)dimList.innerHTML='<div style="padding:24px 8px;text-align:center;color:var(--text-faint);font-size:13px;">加载失败，请先登录后重试</div>';
    });
}
function renderRadar(d){
  var sc=d.dimensionScores||[];
  var total=d.totalAnswered||0;
  var avg=d.avgScore||0;
  var sum=0,weakCount=0;
  sc.forEach(function(s){sum+=(s.score||0);if((s.score||0)<6)weakCount++;});

  if(statAvg)statAvg.innerHTML=avg+'<span class="stat-unit">/10</span>';
  if(statTotal)statTotal.innerHTML=total+'<span class="stat-unit">题</span>';
  if(statWeak)statWeak.innerHTML=weakCount+'<span class="stat-unit">个</span>';
  if(statSum)statSum.innerHTML=sum+'<span class="stat-unit">/70</span>';

  if(dimList){
    dimList.innerHTML=sc.map(function(s){
      var meta=DIM_META[s.dimension]||{label:s.dimension};
      var score=s.score||0;
      var weak=score<6;
      return '<div class="dim-item'+(weak?'':' strong')+'">'
        +'<div class="dim-row"><span class="dim-name">'+escapeHtml(meta.label)
        +(weak?'<span class="dim-weak">需加强</span>':'')
        +'</span><span class="dim-score">'+score+'/10</span></div>'
        +'<div class="dim-bar"><div class="dim-fill" style="width:'+Math.min(score*10,100)+'%"></div></div>'
        +'</div>';
    }).join('');
  }

  var readiness=Math.round(sum/70*100);
  if(readinessFill)readinessFill.style.width=readiness+'%';
  if(readinessValue)readinessValue.textContent=readiness+'%';

  if(learnGrid){
    var sorted=sc.slice().sort(function(a,b){return (a.score||0)-(b.score||0);});
    learnGrid.innerHTML=sorted.slice(0,4).map(function(s){
      var meta=DIM_META[s.dimension];
      if(!meta)return '';
      return '<div class="learn-item">'
        +'<div class="learn-item-head"><span class="learn-item-title">'+escapeHtml(meta.label)+'</span>'
        +'<span class="learn-item-tag">'+escapeHtml(meta.learn.tag)+'</span></div>'
        +'<div class="learn-item-desc">'+escapeHtml(meta.learn.desc)+'</div>'
        +'</div>';
    }).join('');
  }

  drawRadar(sc);
}
function drawRadar(sc){
  if(!radarCanvas)return;
  var cv=radarCanvas,ctx=cv.getContext('2d');
  var W=cv.width,H=cv.height;
  ctx.clearRect(0,0,W,H);
  var cx=W/2,cy=H/2;
  var R=Math.min(W,H)/2-58;
  var n=sc.length||7;
  function pt(i,r){
    var ang=-Math.PI/2+i*2*Math.PI/n;
    return [cx+Math.cos(ang)*r,cy+Math.sin(ang)*r];
  }
  // 网格 5 层
  ctx.strokeStyle='#e2e8f0';
  ctx.lineWidth=1;
  for(var lv=1;lv<=5;lv++){
    var rr=R*lv/5;
    ctx.beginPath();
    for(var i=0;i<n;i++){
      var p=pt(i,rr);
      if(i===0)ctx.moveTo(p[0],p[1]);else ctx.lineTo(p[0],p[1]);
    }
    ctx.closePath();
    ctx.stroke();
  }
  // 轴线
  for(var i=0;i<n;i++){
    var p=pt(i,R);
    ctx.beginPath();ctx.moveTo(cx,cy);ctx.lineTo(p[0],p[1]);ctx.stroke();
  }
  // 标签 + 分数
  ctx.textBaseline='middle';
  for(var i=0;i<n;i++){
    var meta=DIM_META[sc[i].dimension]||{label:sc[i].dimension};
    var ang=-Math.PI/2+i*2*Math.PI/n;
    var cos=Math.cos(ang);
    var p=pt(i,R+30);
    if(cos>0.3)ctx.textAlign='left';
    else if(cos<-0.3)ctx.textAlign='right';
    else ctx.textAlign='center';
    ctx.fillStyle='#44403c';
    ctx.font='12px -apple-system,"PingFang SC","Microsoft YaHei",sans-serif';
    ctx.fillText(meta.label,p[0],p[1]-8);
    ctx.fillStyle='#a8a29e';
    ctx.font='11px -apple-system,"PingFang SC","Microsoft YaHei",sans-serif';
    ctx.fillText((sc[i].score||0)+'/10',p[0],p[1]+8);
  }
  // 数据多边形
  var scores=sc.map(function(s){return (s.score||0)/10;});
  var hasData=scores.some(function(s){return s>0;});
  if(hasData){
    ctx.beginPath();
    for(var i=0;i<n;i++){
      var p=pt(i,R*scores[i]);
      if(i===0)ctx.moveTo(p[0],p[1]);else ctx.lineTo(p[0],p[1]);
    }
    ctx.closePath();
    ctx.fillStyle='rgba(99,102,241,0.12)';
    ctx.strokeStyle='#6366f1';
    ctx.lineWidth=2;
    ctx.fill();
    ctx.stroke();
    ctx.fillStyle='#6366f1';
    for(var i=0;i<n;i++){
      var p=pt(i,R*scores[i]);
      ctx.beginPath();ctx.arc(p[0],p[1],3.5,0,Math.PI*2);ctx.fill();
    }
  }
  // 中心点
  ctx.beginPath();
  ctx.arc(cx,cy,4,0,Math.PI*2);
  ctx.fillStyle='#fff';
  ctx.fill();
  ctx.strokeStyle='#6366f1';
  ctx.lineWidth=2;
  ctx.stroke();
}

// ===== resume diagnosis =====
async function diagnoseResume(){
  var text=resumeInput.value.trim();
  if(!text){resumeStatus.textContent='请先粘贴简历内容';return;}
  resumeDiagnoseBtn.disabled=true;resumeDiagnoseBtn.textContent='诊断中...';
  resumeStatus.textContent='';resumeResults.innerHTML='';
  try{
    var res=await fetch('/api/resume',{method:'POST',headers:{'Content-Type':'application/json'},credentials:'include',body:JSON.stringify({content:text,images:resumePageImages})});
    var data=await res.json();
    if(!res.ok)throw new Error(typeof data.error==='string'?data.error:(data.error&&data.error.message)||('HTTP '+res.status));
    renderResumeDiagnosis(data);
    resumeStatus.textContent=(data.mode==='multimodal'?'视觉 + 文字':'纯文字')+'诊断完成';
  }catch(e){
    resumeResults.innerHTML='<div class="diag-empty" style="color:#b91c1c">诊断失败: '+escapeHtml(e.message)+'</div>';
  }finally{
    resumeDiagnoseBtn.disabled=false;resumeDiagnoseBtn.textContent='多模态诊断';
  }
}
function renderResumeDiagnosis(result){
  var items=result.diagnosis||[];
  if(!items.length){resumeResults.innerHTML='<div class="diag-empty">没有可展示的诊断结果</div>';return;}
  var html='<div class="diag-item"><div class="diag-head"><span>综合评价</span><span class="diag-score">'+(result.overallScore||0)+'/100</span></div>'
    +'<div class="diag-suggestions">'+escapeHtml(result.summary||'')+'</div></div>';
  html+=items.map(function(it){
    return '<div class="diag-item">'
      +'<div class="diag-head"><span>'+escapeHtml(it.section||'(未命名段落)')+'</span><span class="diag-score">'+it.score+'/10</span></div>'
      +(it.evidence&&it.evidence.length?'<div class="diag-suggestions">证据：'+escapeHtml(it.evidence.join(' · '))+'</div>':'')
      +(it.issues&&it.issues.length?'<div class="diag-issues">问题：'+escapeHtml(it.issues.join(' · '))+'</div>':'')
      +'<div class="diag-suggestions">建议：'+escapeHtml((it.suggestions||[]).join(' · '))+'</div>'
      +(it.rewrite?'<div class="diag-suggestions">改写：'+escapeHtml(it.rewrite)+'</div>':'')
      +'</div>';
  }).join('');
  if(result.layout){
    html+='<div class="diag-item"><div class="diag-head"><span>版式评价</span><span class="diag-score">'+(result.layout.score||0)+'/10</span></div>'
      +'<div class="diag-suggestions">'+escapeHtml(result.layout.summary||'')+'</div></div>';
  }
  resumeResults.innerHTML=html;
}

// ===== JD match =====
async function runMatch(){
  var jd=jdInput.value.trim();
  var resume=matchResumeInput.value.trim();
  if(!jd||!resume){matchStatus.textContent='请同时填写 JD 和简历内容';return;}
  matchBtn.disabled=true;matchBtn.textContent='匹配中...';
  matchStatus.textContent='';matchResults.innerHTML='';
  try{
    var res=await fetch('/api/match',{method:'POST',headers:{'Content-Type':'application/json'},credentials:'include',body:JSON.stringify({jd:jd,resume:resume})});
    var data=await res.json();
    if(!res.ok)throw new Error(data.error||('HTTP '+res.status));
    renderMatchResult(data);
    matchStatus.textContent='匹配完成';
  }catch(e){
    matchResults.innerHTML='<div class="diag-empty" style="color:#b91c1c">匹配失败: '+escapeHtml(e.message)+'</div>';
  }finally{
    matchBtn.disabled=false;matchBtn.textContent='开始匹配';
  }
}
function renderMatchResult(d){
  var pct=d.score||0;
  var html='';
  html+='<div class="match-score-wrap">'
    +'<div class="match-score-circle" style="--score-pct:'+pct+'%"><span class="match-score-num">'+pct+'<span class="match-score-label">%</span></span></div>'
    +'<div class="match-detail"><div class="match-level">匹配度 '+pct+'%'+(d.level?' · '+escapeHtml(d.level):'')+'</div>'
    +'<div class="match-focus">'+(d.focus&&d.focus.length?escapeHtml(d.focus.join(' · ')):'')+'</div></div></div>';
  html+='<div class="match-section-label">✅ 已匹配关键词</div>';
  html+='<div class="match-tags">';
  if(d.matched&&d.matched.length){
    d.matched.forEach(function(k){html+='<span class="match-tag matched">'+escapeHtml(k)+'</span>';});
  }else{html+='<span style="font-size:13px;color:var(--text-faint)">无</span>';}
  html+='</div>';
  html+='<div class="match-section-label">⚠ 缺失关键词</div>';
  html+='<div class="match-tags">';
  if(d.missing&&d.missing.length){
    d.missing.forEach(function(k){html+='<span class="match-tag missing">'+escapeHtml(k)+'</span>';});
  }else{html+='<span style="font-size:13px;color:var(--text-faint)">无</span>';}
  html+='</div>';
  if(d.suggestions&&d.suggestions.length){
    html+='<div class="match-section-label">💡 改进建议</div>';
    html+='<ul class="match-suggestions">';
    d.suggestions.forEach(function(s){html+='<li>'+escapeHtml(s)+'</li>';});
    html+='</ul>';
  }
  matchResults.innerHTML=html;
}

// ===== file upload =====
async function parseUploadedFile(file,renderPages){
  var name=(file.name||'').toLowerCase();
  if(name.endsWith('.pdf')){
    try{
      var reader=await import('/pdf-reader.mjs');
      return await reader.readPDF(file,{renderPages:!!renderPages,maxRenderedPages:3});
    }catch(browserError){
      // Keep the original Go parser as a compatibility fallback. PDF.js is
      // the primary path because its local CMaps correctly decode CID fonts.
      try{
        return await parseUploadedFileOnServer(file);
      }catch(serverError){
        throw new Error('PDF.js: '+browserError.message+'; 服务端回退: '+serverError.message);
      }
    }
  }
  return parseUploadedFileOnServer(file);
}
async function parseUploadedFileOnServer(file){
  var fd=new FormData();fd.append('file',file);
  var res=await fetch('/api/parse-pdf',{method:'POST',body:fd,credentials:'include'});
  var data=await res.json();
  if(!res.ok)throw new Error(data.error||('HTTP '+res.status));
  return data;
}
async function uploadFile(file, statusEl, targetTextarea){
  if(!file)return;
  if(file.size>10*1024*1024){statusEl.textContent='文件超过 10MB';return;}
  statusEl.textContent='解析中...';
  try{
    var isResume=targetTextarea===resumeInput;
    var data=await parseUploadedFile(file,isResume);
    targetTextarea.value=data.text||'';
    if(isResume)resumePageImages=data.pageImages||[];
    statusEl.textContent='已解析 '+(data.pages?data.pages+' 页 · ':'')+(data.text?data.text.length:'0')+' 字'
      +(isResume&&resumePageImages.length?' · 视觉 '+resumePageImages.length+' 页':'');
  }catch(e){
    if(targetTextarea===resumeInput)resumePageImages=[];
    statusEl.textContent='解析失败: '+e.message;
  }
}
function bindUpload(btn, fileInput, statusEl, targetTextarea){
  if(!btn||!fileInput||!statusEl||!targetTextarea)return;
  btn.addEventListener('click',function(){fileInput.click();});
  fileInput.addEventListener('change',function(){
    if(fileInput.files.length)uploadFile(fileInput.files[0],statusEl,targetTextarea);
  });
  var card=targetTextarea.closest('.radar-card');
  if(!card)return;
  card.addEventListener('dragover',function(e){e.preventDefault();card.classList.add('upload-drag-over');});
  card.addEventListener('dragleave',function(){card.classList.remove('upload-drag-over');});
  card.addEventListener('drop',function(e){
    e.preventDefault();card.classList.remove('upload-drag-over');
    if(e.dataTransfer.files.length)uploadFile(e.dataTransfer.files[0],statusEl,targetTextarea);
  });
}

// ===== chat file upload =====
function initChatUpload(){
  if(!uploadBtn||!chatFileInput||!input)return;
  uploadBtn.addEventListener('click',function(){chatFileInput.click();});
  chatFileInput.addEventListener('change',function(){
    if(chatFileInput.files.length)uploadToChat(chatFileInput.files[0]);
    chatFileInput.value='';
  });
  // drag-and-drop on chat area
  var main=document.querySelector('.main');
  if(!main)return;
  main.addEventListener('dragover',function(e){e.preventDefault();main.classList.add('upload-drag-over');});
  main.addEventListener('dragleave',function(e){
    if(e.target===main)main.classList.remove('upload-drag-over');
  });
  main.addEventListener('drop',function(e){
    e.preventDefault();main.classList.remove('upload-drag-over');
    if(e.dataTransfer.files.length)uploadToChat(e.dataTransfer.files[0]);
  });
}
function formatSize(bytes){
  if(bytes<1024)return bytes+' B';
  if(bytes<1048576)return (bytes/1024).toFixed(1)+' KB';
  return (bytes/1048576).toFixed(1)+' MB';
}
async function uploadToChat(file){
  if(!file)return;
  if(file.size>10*1024*1024){showToast('文件超过 10MB');return;}
  var orig=uploadBtn.innerHTML;
  uploadBtn.innerHTML='<span class="upload-spin">⏳</span>';
  uploadBtn.disabled=true;
  try{
    var data=await parseUploadedFile(file,false);
    var text=data.text||'';
    if(!text.trim()){showToast('文件解析为空');return;}
    state.files.push({name:file.name,size:file.size,content:text});
    renderFileChips();
    showToast('已添加 '+file.name);
  }catch(e){
    showToast('解析失败: '+e.message);
  }finally{
    uploadBtn.innerHTML=orig;
    uploadBtn.disabled=false;
  }
}
function renderFileChips(){
  if(!fileChips)return;
  fileChips.innerHTML='';
  state.files.forEach(function(f,i){
    var chip=document.createElement('div');
    chip.className='file-chip';
    chip.innerHTML='<span class="file-chip-icon">📄</span><span class="file-chip-name">'+escapeHtml(f.name)+'</span><span class="file-chip-size">'+formatSize(f.size)+'</span><button class="file-chip-remove" data-idx="'+i+'" type="button">×</button>';
    chip.querySelector('.file-chip-remove').addEventListener('click',function(e){
      e.stopPropagation();
      state.files.splice(i,1);
      renderFileChips();
    });
    fileChips.appendChild(chip);
  });
}
function escapeHtml(s){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');}
function showToast(msg){
  var t=document.createElement('div');
  t.className='toast';t.textContent=msg;
  document.body.appendChild(t);
  setTimeout(function(){t.classList.add('toast-out');},2000);
  setTimeout(function(){if(t.parentNode)t.parentNode.removeChild(t);},2500);
}

// ===== submit & stream =====
async function submit(){
  var text=input.value.trim();
  var attachedFiles=state.files.slice(); // 保存引用用于显示
  // 拼接文件内容发送给后端
  if(state.files.length>0){
    var fileTexts=state.files.map(function(f){
      return '[上传文件: '+f.name+']\n'+f.content;
    }).join('\n\n');
    if(text) text=text+'\n\n'+fileTexts;
    else text=fileTexts;
    state.files=[];
    renderFileChips();
  }
  if(!text||state.streaming)return;
  if(!state.currentSid)createSession();
  var session=getSession(state.currentSid);
  if(!session)return;
  var userMsg={role:'user',content:text,files:attachedFiles};
  session.messages.push(userMsg);
  session.updatedAt=Date.now();
  if(session.title==='新会谈'||!session.title)session.title=autoTitle(input.value.trim()||text);
  saveSessions();
  renderSessionsBar();
  welcome.classList.add('hidden');
  chat.classList.remove('hidden');
  appendMessageEl('user',input.value.trim()||'',{streaming:false,files:attachedFiles});
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
    body:JSON.stringify({message:text,sessionId:state.currentSid||'',model:state.model}),
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
      }catch(e){console.warn('parse fail:',payload,e);}
    }
  }
  if(onDone)onDone();
}
function handleEvent(evt,accum,session){
  switch(evt.type){
    case 'text_delta':
    accum.text+=evt.content||'';
    if(accum.text.length===evt.content.length||0){
      var last=chat.querySelector('.msg.assistant.streaming');
      if(last){var tb=last.querySelector('.think-block');if(tb)tb.open=false;}
    }
    updateStreamingContent(accum);
    break;
    case 'thinking_delta':accum.thinking+=evt.content||'';updateStreamingContent(accum);break;
    case 'tool_call':appendToolEvent(evt.name,evt.input);break;
    case 'tool_result':appendToolResult(evt.name,evt.result);break;
    case 'error':appendSystemMessage('错误: '+(evt.error||'未知错误'));break;
    case 'session':
      var serverSid=evt.sessionId;
      if(serverSid&&serverSid!==state.currentSid){
        var existing=state.sessions[serverSid];
        if(existing&&existing.messages&&existing.messages.length>0){
          state.currentSid=serverSid;
          localStorage.setItem(STORAGE.current,serverSid);
        }else{
          var oldSid=state.currentSid;
          if(oldSid&&state.sessions[oldSid]){
            state.sessions[serverSid]=state.sessions[oldSid];
            state.sessions[serverSid].id=serverSid;
            delete state.sessions[oldSid];
          }
          state.currentSid=serverSid;
          localStorage.setItem(STORAGE.current,serverSid);
          saveSessions();
        }
      }
      break;
  }
}
function onStreamDone(accum,session){
  finishStreamingContent(accum);
  session.messages.push({role:'assistant',content:accum.text,thinking:accum.thinking||''});
  session.updatedAt=Date.now();
  saveSessions();
  renderSessionsBar();
  renderSidebar();
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
  newSessionBtn.addEventListener('click',function(){
    var s=createSession();
    welcome.classList.remove('hidden');
    chat.classList.add('hidden');
    chat.innerHTML='';
    renderSessionsBar();
    renderSidebar();
  });
  if(authLink){
    authLink.addEventListener('click',function(e){
      if(state.user){
        e.preventDefault();
        showRadar();
      }
      // 未登录时保持默认跳转 /login
    });
  }
  if(radarBack)radarBack.addEventListener('click',hideRadar);
  if(radarRefresh)radarRefresh.addEventListener('click',fetchRadar);
  // 用户主页 tab 切换
  if(radarTabs){
    radarTabs.addEventListener('click',function(e){
      var tab=e.target.closest('.radar-tab');
      if(!tab)return;
      switchRadarTab(tab.getAttribute('data-tab'));
    });
  }
  // 简历诊断
  if(resumeDiagnoseBtn)resumeDiagnoseBtn.addEventListener('click',diagnoseResume);
  if(resumeInput)resumeInput.addEventListener('input',function(){resumePageImages=[];});
  if(resumeAIBtn)resumeAIBtn.addEventListener('click',function(){
    var text=resumeInput.value.trim();
    if(!text){resumeStatus.textContent='请先上传或粘贴简历';return;}
    jumpToChat('请基于以下简历为我进行个性化模拟面试。先调用 mock_interview，resumeText 必须传入完整简历，每次只问一道题，并优先核验项目职责、技术决策和量化结果。\n\n【候选人简历】\n'+text);
  });
  // JD匹配
  if(matchBtn)matchBtn.addEventListener('click',runMatch);
  if(matchAIBtn)matchAIBtn.addEventListener('click',function(){
    var jd=jdInput.value.trim();
    var resume=matchResumeInput.value.trim();
    jumpToChat('请帮我对比以下 JD 和我的简历，逐节生成完整的面试准备文档：\n\n【JD】\n'+jd+'\n\n【简历】\n'+resume);
  });
  // 文件上传
  bindUpload(resumeUploadBtn,resumeFileInput,resumeUploadStatus,resumeInput);
  bindUpload(jdUploadBtn,jdFileInput,jdUploadStatus,jdInput);
  bindUpload(matchResumeUploadBtn,matchResumeFileInput,matchResumeUploadStatus,matchResumeInput);
  bindSidebar();
}
// ===== init =====
function init(){
  loadAll();
  renderModelMenu();
  bindEvents();
  initChatUpload();
  fetchUser();
  if(state.currentSid&&state.sessions[state.currentSid]){
    renderMessages(state.sessions[state.currentSid].messages);
  }else{
    welcome.classList.remove('hidden');
    chat.classList.add('hidden');
  }
  renderSessionsBar();
  renderSidebar();
}
if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',init);}else{init();}

