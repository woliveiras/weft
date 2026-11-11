import {expect,it,vi} from 'vitest';
import {canDiscover,saveAndDiscover} from './mcpDiscovery';
import type {MCPConnection,MCPConnectionInput} from '../api/types';
const c={id:'team',enabled:true,tools:[]} as unknown as MCPConnection;
it('discovers only enabled connections with available authorization',()=>{
 expect(canDiscover(c)).toBe(true);
 expect(canDiscover({...c,enabled:false})).toBe(false);
 for(const status of ['authorization_required','revoked','error']) expect(canDiscover({...c,oauth:{status} as MCPConnection['oauth']})).toBe(false);
 expect(canDiscover({...c,oauth:{status:'authorized'} as MCPConnection['oauth']})).toBe(true);
});
it('saves before discovering and preserves a saved connection on discovery failure',async()=>{
 const saved=vi.fn();const client={save:vi.fn().mockResolvedValue(c),refresh:vi.fn().mockImplementation(()=>{expect(saved).toHaveBeenCalledWith(c);throw new Error('Unavailable');})};
 const result=await saveAndDiscover({} as MCPConnectionInput,2,saved,client);
 expect(client.save).toHaveBeenCalledWith({},2);expect(result).toEqual({connection:c,discoveryError:'Unavailable'});
});
it('never discovers when save fails or the connection is disabled',async()=>{
 const client={save:vi.fn().mockRejectedValue(new Error('Conflict')),refresh:vi.fn()};
 await expect(saveAndDiscover({} as MCPConnectionInput,2,vi.fn(),client)).rejects.toThrow('Conflict');
 expect(client.refresh).not.toHaveBeenCalled();
 client.save.mockResolvedValue({...c,enabled:false});
 await saveAndDiscover({} as MCPConnectionInput,2,vi.fn(),client);expect(client.refresh).not.toHaveBeenCalled();
});
it.each([false,true])('discovers without inferring agent grants (credential configured: %s)',async credentialConfigured=>{
 const connection={...c,credentialConfigured};
 const ready={...connection,status:'ready',tools:[]};
 const client={save:vi.fn().mockResolvedValue(connection),refresh:vi.fn().mockResolvedValue(ready)};
 expect(await saveAndDiscover({} as MCPConnectionInput,undefined,vi.fn(),client)).toEqual({connection:ready});
 expect(client.refresh).toHaveBeenCalledExactlyOnceWith('team','tools/refresh');
});
it('defers discovery until OAuth authorization, but refreshes an authorized connection',async()=>{
 const connection={...c,oauth:{status:'authorization_required'} as MCPConnection['oauth']};
 const client={save:vi.fn().mockResolvedValue(connection),refresh:vi.fn().mockResolvedValue({...c,status:'ready'})};
 await saveAndDiscover({} as MCPConnectionInput,undefined,vi.fn(),client);expect(client.refresh).not.toHaveBeenCalled();
 client.save.mockResolvedValue({...connection,oauth:{status:'authorized'}});
 await saveAndDiscover({} as MCPConnectionInput,2,vi.fn(),client);expect(client.refresh).toHaveBeenCalledOnce();
});
