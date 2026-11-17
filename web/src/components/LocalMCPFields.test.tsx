import {expect,it} from 'vitest';
import {parseProcessEnvironment} from './LocalMCPFields';
it('parses explicit environment replacement and clearing without reflecting invalid secrets',()=>{
 expect(parseProcessEnvironment(' ')).toBeUndefined();expect(parseProcessEnvironment('{}')).toEqual({});expect(parseProcessEnvironment('{"TOKEN":"fixture"}')).toEqual({TOKEN:'fixture'});
 for(const value of ['null','[]','{"TOKEN":42}','{"BAD=KEY":"secret"}','secret-invalid-json'])expect(()=>parseProcessEnvironment(value)).toThrow(/^Environment must be/);
});
