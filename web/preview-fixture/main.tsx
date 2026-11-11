import {useEffect, useState} from 'react';
import {createRoot} from 'react-dom/client';
function App() {
 const [count,setCount]=useState(0);const [result,setResult]=useState('Not saved');const [socket,setSocket]=useState('Connecting');
 useEffect(()=>{const ws=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/ws`);ws.onopen=()=>ws.send('preview websocket');ws.onmessage=e=>setSocket(e.data);ws.onerror=()=>setSocket('WebSocket failed');return()=>ws.close();},[]);
 return <main><h1>Sandbox review</h1><button onClick={()=>setCount(n=>n+1)}>Count: {count}</button><button onClick={async()=>{const r=await fetch('/api/value',{method:'POST',headers:{Authorization:'Bearer application-token'},body:String(count)});setResult(await r.text());}}>Save test value</button><p>{result}</p><p>{socket}</p><a href="/nested">Nested route</a></main>;
}
createRoot(document.getElementById('root')!).render(<App/>);
