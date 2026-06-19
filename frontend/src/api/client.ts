export interface ApiError {code:string;message:string}
export interface Envelope<T>{ok:boolean;data:T;error?:ApiError}
export class ApiRequestError extends Error{constructor(message:string,public status:number,public code:string){super(message)}}
export async function api<T>(path:string,init?:RequestInit):Promise<T>{const response=await fetch(path,{credentials:'same-origin',...init,headers:{'Content-Type':'application/json',...(init?.headers||{})}});const body=await response.json() as Envelope<T>;if(!response.ok||!body.ok)throw new ApiRequestError(body.error?.message||`请求失败 (${response.status})`,response.status,body.error?.code||'REQUEST_FAILED');return body.data}
export const post=<T>(path:string,data:unknown={})=>api<T>(path,{method:'POST',body:JSON.stringify(data)})
export const put=<T>(path:string,data:unknown)=>api<T>(path,{method:'PUT',body:JSON.stringify(data)})
