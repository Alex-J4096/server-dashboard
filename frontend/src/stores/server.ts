import {defineStore} from 'pinia'
import {api,post} from '../api/client'
import type {ServerStatus} from '../api/types'
export const useServerStore=defineStore('server',{state:()=>({status:{state:'stopped',uptime_seconds:0} as ServerStatus,loading:false}),actions:{async refresh(){this.status=await api<ServerStatus>('/api/server/status')},async action(name:'start'|'stop'|'restart'){this.loading=true;try{await post(`/api/server/${name}`);await this.refresh()}finally{this.loading=false}}}})
